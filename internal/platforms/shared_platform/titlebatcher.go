package shared_platform

import (
	"context"
	"seanime/internal/api/anilist"
	"seanime/internal/util"
	"sync"
	"time"
)

const (
	// titleBatchWindow is how long a batch waits for more lookups before it's sent.
	titleBatchWindow = 50 * time.Millisecond
	// titleBatchSize sends a batch as soon as it fills AniList's largest page.
	titleBatchSize = 50
	// titleBatchTimeout ends a stalled batch, since the HTTP client has none. It outlasts a
	// rate-limit block.
	titleBatchTimeout = 2 * time.Minute
)

var (
	baseAnimeBatcher = newTitleBatcher(anilist.AnilistClient.BaseAnimeByIDs,
		func(m *anilist.BaseAnime) int { return m.ID },
		func(media ...*anilist.BaseAnime) { CurrentTitleCache().PutAnime(media...) })
	baseMangaBatcher = newTitleBatcher(anilist.AnilistClient.BaseMangaByIDs,
		func(m *anilist.BaseManga) int { return m.ID },
		func(media ...*anilist.BaseManga) { CurrentTitleCache().PutManga(media...) })
	completeAnimeBatcher = newTitleBatcher(anilist.AnilistClient.CompleteAnimeByIDs,
		func(m *anilist.CompleteAnime) int { return m.ID },
		func(media ...*anilist.CompleteAnime) { CurrentTitleCache().PutCompleteAnime(media...) })
)

// titleBatcher combines single-title lookups from every profile into ID-list queries. Title data is
// public, so a batch is sent with the login of the profile whose lookup started it.
type titleBatcher[M any] struct {
	fetch func(client anilist.AnilistClient, ctx context.Context, ids []int) ([]*M, error)
	id    func(*M) int
	save  func(...*M)

	mu      sync.Mutex
	calls   map[int]*titleCall[M] // the latest lookup pending or in flight, by title ID
	pending *titleBatch[M]
}

type titleCall[M any] struct {
	batch *titleBatch[M]
	done  chan struct{}
	media *M
	err   error
}

type titleBatch[M any] struct {
	profileID  string
	client     anilist.AnilistClient
	ids        []int
	calls      []*titleCall[M] // one per ID
	background bool            // every lookup in it is background work
	timer      *time.Timer
	errTaken   bool // a caller has already been handed the batch's error
}

// sharedBatchError is a failed batch's error as every caller but the first sees it, so one failed
// request counts once toward marking AniList down.
type sharedBatchError struct{ error }

func (e sharedBatchError) Unwrap() error { return e.error }

func newTitleBatcher[M any](fetch func(anilist.AnilistClient, context.Context, []int) ([]*M, error), id func(*M) int, save func(...*M)) *titleBatcher[M] {
	return &titleBatcher[M]{fetch: fetch, id: id, save: save, calls: make(map[int]*titleCall[M])}
}

// get returns id's record from the next batch, joining a lookup for it already pending or in
// flight. When the batch doesn't return it, or another login's token was rejected, single looks it
// up with the caller's own login, which may see titles another login can't, such as adult ones.
func (b *titleBatcher[M]) get(ctx context.Context, client anilist.AnilistClient, id int, single func() (*M, error)) (*M, error) {
	background := anilist.IsBackgroundPriority(ctx)
	b.mu.Lock()
	call, ok := b.calls[id]
	// A browsing lookup doesn't wait behind a background batch that's already sent.
	if ok && !background && call.batch.background && call.batch != b.pending {
		ok = false
	}
	if !ok {
		call = &titleCall[M]{done: make(chan struct{})}
		b.calls[id] = call
		b.addLocked(ctx, client, id, call, background)
	} else if !background && call.batch == b.pending {
		call.batch.background = false
	}
	b.mu.Unlock()

	select {
	case <-call.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if call.media != nil {
		return call.media, nil
	}
	if call.err != nil && (client == call.batch.client || !isAnilistAuthError(call.err)) {
		return nil, b.batchError(call.batch, call.err)
	}
	media, err := single()
	if err == nil {
		b.save(media)
	}
	return media, err
}

// batchError hands the first caller the batch's error and every later one a sharedBatchError.
func (b *titleBatcher[M]) batchError(batch *titleBatch[M], err error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if batch.errTaken {
		return sharedBatchError{err}
	}
	batch.errTaken = true
	return err
}

func (b *titleBatcher[M]) addLocked(ctx context.Context, client anilist.AnilistClient, id int, call *titleCall[M], background bool) {
	batch := b.pending
	if batch == nil {
		batch = &titleBatch[M]{profileID: util.ProfileIDFromContext(ctx), client: client, background: true}
		batch.timer = time.AfterFunc(titleBatchWindow, func() { b.send(batch) })
		b.pending = batch
	}
	batch.ids = append(batch.ids, id)
	batch.calls = append(batch.calls, call)
	batch.background = batch.background && background
	call.batch = batch
	if len(batch.ids) == titleBatchSize {
		b.pending = nil
		if batch.timer.Stop() {
			go b.send(batch)
		}
	}
}

// send fetches the batch and hands each lookup its record. Records are saved before any caller
// sees the result, since a caller's failure may mark AniList down, which stops saves.
func (b *titleBatcher[M]) send(batch *titleBatch[M]) {
	b.mu.Lock()
	if b.pending == batch {
		b.pending = nil
	}
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(util.ContextWithProfileID(context.Background(), batch.profileID), titleBatchTimeout)
	defer cancel()
	if batch.background {
		ctx = anilist.WithBackgroundPriority(ctx)
	}
	media, err := b.fetch(batch.client, ctx, batch.ids)
	b.save(media...)

	found := make(map[int]*M, len(media))
	for _, m := range media {
		if m != nil {
			found[b.id(m)] = m
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, call := range batch.calls {
		id := batch.ids[i]
		if b.calls[id] == call {
			delete(b.calls, id)
		}
		call.media = found[id]
		if call.media == nil {
			call.err = err
		}
		close(call.done)
	}
}
