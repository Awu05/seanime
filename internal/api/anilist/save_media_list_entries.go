package anilist

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
)

// MediaListEntryUpdate is one list change for SaveMediaListEntries. A progress-only update sends the
// same fields as UpdateMediaListEntryProgress; any other sends the same as UpdateMediaListEntry.
type MediaListEntryUpdate struct {
	MediaID      int
	Status       *MediaListStatus
	ScoreRaw     *int
	Progress     *int
	StartedAt    *FuzzyDateInput
	CompletedAt  *FuzzyDateInput
	ProgressOnly bool
}

type listEntryArg struct {
	name, gqlType string
	value         any
}

func (u MediaListEntryUpdate) args() []listEntryArg {
	if u.ProgressOnly {
		return []listEntryArg{{"mediaId", "Int", u.MediaID}, {"progress", "Int", u.Progress}, {"status", "MediaListStatus", u.Status}}
	}
	return []listEntryArg{
		{"mediaId", "Int", u.MediaID},
		{"status", "MediaListStatus", u.Status},
		{"scoreRaw", "Int", u.ScoreRaw},
		{"progress", "Int", u.Progress},
		{"startedAt", "FuzzyDateInput", u.StartedAt},
		{"completedAt", "FuzzyDateInput", u.CompletedAt},
	}
}

// SaveMediaListEntries saves every update in one request. It returns an error per update, nil when
// AniList saved it, or an error for the whole request when that failed.
func (ac *AnilistClientImpl) SaveMediaListEntries(ctx context.Context, updates []MediaListEntryUpdate) ([]error, error) {
	if !ac.IsAuthenticated() {
		return nil, ErrNotAuthenticated
	}
	if len(updates) == 0 {
		return nil, nil
	}
	ac.logger.Debug().Int("count", len(updates)).Msg("anilist: Saving media list entries")

	query, variables := saveMediaListEntriesRequest(updates)
	body, err := json.Marshal(map[string]any{"operationName": "SaveMediaListEntries", "query": query, "variables": variables})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, alApiUrl(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if err := initAnilistReq(ctx, req, ac.token); err != nil {
		return nil, err
	}

	resp, _, err := doAniListRequestWithRetries(alHttpClient(), req, sharedAniListPacer, sleepWithContext, func(waitSeconds int) {
		notifyAniListRateLimit(ac.logger, waitSeconds)
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	return saveMediaListEntriesResults(len(updates), resp.StatusCode, data)
}

// saveMediaListEntriesRequest builds one mutation with a SaveMediaListEntry per update, aliased u0,
// u1, ... so AniList's errors name the update they belong to.
func saveMediaListEntriesRequest(updates []MediaListEntryUpdate) (string, map[string]any) {
	var params []string
	var fields strings.Builder
	variables := make(map[string]any)
	for i, u := range updates {
		var args []string
		for _, arg := range u.args() {
			variable := arg.name + strconv.Itoa(i)
			params = append(params, "$"+variable+": "+arg.gqlType)
			args = append(args, arg.name+": $"+variable)
			variables[variable] = arg.value
		}
		fmt.Fprintf(&fields, "\tu%d: SaveMediaListEntry(%s) {\n\t\tid\n\t}\n", i, strings.Join(args, ", "))
	}
	return "mutation SaveMediaListEntries (" + strings.Join(params, ", ") + ") {\n" + fields.String() + "}", variables
}

// saveMediaListEntriesResults matches AniList's errors to the updates they name. An error naming no
// update, or a failed response without errors, fails the whole request.
func saveMediaListEntriesResults(count int, statusCode int, body []byte) ([]error, error) {
	var res struct {
		Errors []struct {
			Message string `json:"message"`
			Path    []any  `json:"path"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("anilist: failed to decode response (status %d): %w", statusCode, err)
	}
	if len(res.Errors) == 0 && (statusCode < 200 || statusCode > 299) {
		return nil, fmt.Errorf("anilist: request failed with status %d", statusCode)
	}
	results := make([]error, count)
	for _, e := range res.Errors {
		i := updateIndex(e.Path, count)
		if i < 0 {
			return nil, fmt.Errorf("anilist: %s (status %d)", e.Message, statusCode)
		}
		results[i] = errors.Join(results[i], errors.New(e.Message))
	}
	return results, nil
}

// updateIndex returns which update an error path's alias names, or -1.
func updateIndex(path []any, count int) int {
	if len(path) == 0 {
		return -1
	}
	alias, _ := path[0].(string)
	i, err := strconv.Atoi(strings.TrimPrefix(alias, "u"))
	if !strings.HasPrefix(alias, "u") || err != nil || i < 0 || i >= count {
		return -1
	}
	return i
}
