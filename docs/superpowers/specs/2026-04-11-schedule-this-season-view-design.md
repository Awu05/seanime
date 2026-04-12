# Schedule "This Season" View — Design

## Problem

The schedule page today shows the airing calendar for the user's list plus upcoming episodes, but there's no way to browse the broader AniList season listing from inside seanime. Users who want to discover what's airing this season — or plan for next season, or catch up on last season — have to leave the app or fall back to Advanced Search with manually entered season/year filters.

## Goal

Add a "This Season" view to the existing schedule page that shows an AniList season listing (previous, current, or next season) as a grid of clickable cards, each with poster, title, audience score, synopsis, airing date range, and an add-to-list action that writes back to AniList.

## Non-goals

- No new top-level navigation entry. The Schedule page is the conceptual home.
- No format filter (TV / Movie / OVA / ONA) for MVP. If needed later, it's a one-line addition to the fetch args.
- No dedicated "favorite" (AniList heart) action. The existing add-to-list modal is what "tracks" a show on AniList.
- No search box, no genre filter. Advanced Search already handles those cases.
- No mobile-specific layout. The grid and hover popup inherit whatever responsive behavior `MediaEntryCard` already has.

## Cross-cutting addition — 18+ badge on adult content

As part of this feature, an "18+" badge is added to adult shows wherever they appear on the schedule page. This is only visible when the profile has `EnableAdultContent = true`; when disabled, adult entries are still filtered out entirely as today, so the badge never shows.

**Where the badge appears**:

1. `MediaEntryCard` — a small corner overlay on the poster when `media.isAdult === true`. Because the card is shared infrastructure, this benefits every caller automatically: This Season view, Discover, Advanced Search, Library, Sync, etc.
2. Calendar view entries — both the desktop grid and the mobile list, when `event.isAdult === true`.

**Badge component**: a new small `AdultContentBadge` component in `seanime-web/src/app/(main)/_features/media/_components/` (or inlined in `media-entry-card-components.tsx` if that's where similar small overlays already live). Styling: a compact red/intent badge with "18+" text, positioned in a card corner so it doesn't cover the poster. Picks up the existing badge sizing used elsewhere on the cards.

**Calendar plumbing**:

- `Anime_ScheduleItem` on the backend already exposes `IsAdult bool` (added previously). The generated frontend type `Anime_ScheduleItem.isAdult` is already available.
- The frontend `CalendarEvent` type in `schedule-calendar.tsx` gains a new field `isAdult: boolean`.
- Wherever `CalendarEvent` is constructed from an `Anime_ScheduleItem`, populate `isAdult` from the source.
- Desktop calendar entry renderer reads `event.isAdult` and renders the badge inline next to the title.
- Mobile list entry renderer does the same.

**Card plumbing** for `MediaEntryCard`:

- The card already reads `media.isAdult` for blur purposes. Add the badge element next to the existing badges (library badge, score, progress) so it only appears when `media.isAdult === true`.
- No new prop required — the badge is always shown when the flag is true, globally for every consumer of the card.

**Scope note**: this touches `MediaEntryCard`, which is shared across many pages. The change is purely additive (a new overlay element that only renders when `isAdult === true`), so no other page's behavior changes. If the user has adult content disabled, the backend already filters these entries out before they reach the card, so the badge is effectively dormant.

## Scope summary

- **Placement**: new tab at the top of the schedule page alongside the existing Calendar tab. Calendar is always the default on page load; tab state is not persisted.
- **Season scope**: previous / current / next, switched via a three-way segmented control.
- **Sort**: popularity (default), trending, score, start date — user-selectable via dropdown.
- **Full coverage**: the fetch loops AniList pages server-side until exhausted, returning every anime in the season regardless of count.
- **Card content**: existing `MediaEntryCard` with one new prop that opts into showing synopsis + airing date range inside the hover popup.

---

## Architecture

### Page layout

The schedule page is restructured with a top-level `Tabs` component (the same UI primitive used elsewhere in seanime) containing two tabs:

1. **Calendar tab** — wraps the current page contents unchanged: `MissingEpisodes`, `UpcomingEpisodes`, `ScheduleCalendar`, the `Show all airing` toggle, and the plugin webview slots. No behavior change for existing users.
2. **This Season tab** — new content (see `ThisSeasonView` below).

Tab state lives in a local `useState("calendar")` on the schedule page. It is intentionally not persisted. Every page load returns the user to the Calendar tab.

### Component tree

```
SchedulePage
├─ <Tabs value={tab} onValueChange={setTab}>
│  ├─ <TabsList>
│  │   ├─ Calendar
│  │   └─ This Season
│  └─ <TabsContent>
│     ├─ CalendarTabContent (existing layout)
│     └─ ThisSeasonView (new)
│        ├─ SeasonControls (segmented control + sort dropdown + resolved-season label)
│        └─ MediaCardLazyGrid
│           └─ MediaEntryCard (with showExpandedHoverContent)
```

---

## Components

### `ThisSeasonView` (new)

**File**: `seanime-web/src/app/(main)/schedule/_components/this-season-view.tsx`

**Local state** (all `useState`, not persisted):
- `seasonKind: "previous" | "current" | "next"` — default `"current"`
- `sort: "POPULARITY_DESC" | "TRENDING_DESC" | "SCORE_DESC" | "START_DATE_DESC"` — default `"POPULARITY_DESC"`

**Top control row**:
- Segmented control with three buttons: Previous / Current / Next
- Sort dropdown with four options
- A right-aligned label rendering the resolved season, e.g. "Spring 2026"

**Body**:
- `MediaCardLazyGrid` rendering one `MediaEntryCard` per result
- Each card uses `type="anime"`, `showLibraryBadge`, and a new prop `showExpandedHoverContent={true}` (see below)

**Loading / empty / error states**:
- Loading: skeleton grid of placeholder cards, same component the Discover page uses
- Empty: centered card with "No anime found for {Season Year}"
- Error: the shared error card pattern with a Retry button that re-invokes the query

### `computeSeasonParams` helper

**File**: co-located in `this-season-view.tsx` or a small utility module

**Signature**: `computeSeasonParams(kind, now: Date) => { season: AL_MediaSeason, seasonYear: number }`

**Logic**: maps current month to a base season:
- Jan/Feb/Mar → WINTER
- Apr/May/Jun → SPRING
- Jul/Aug/Sep → SUMMER
- Oct/Nov/Dec → FALL

Then shifts the base by `-1` (previous), `0` (current), or `+1` (next) seasons, rolling the year when crossing the WINTER/FALL boundary. Mirrors the semantics of the backend `GetSeasonInfo` helper but runs client-side so we don't add an unnecessary endpoint round-trip.

### `MediaEntryCard` — one new optional prop

**File**: `seanime-web/src/app/(main)/_features/media/_components/media-entry-card.tsx` (existing)

**New prop**: `showExpandedHoverContent?: boolean` (default `false`, anime-only).

When `true`, the hover popup body renders two additional elements above the existing footer:

1. **Synopsis paragraph** — `media.description` with HTML tags stripped (AniList descriptions contain `<br>`, `<i>`, etc), line-clamped to ~4 lines so it fits inside the existing popup container without expanding it.
2. **Airing date range** — formatted from `media.startDate` + `media.endDate`:
   - Both present: `"Jan 5 – Mar 28, 2026"`
   - Start only: `"Starts Jan 5, 2026"`
   - End only (rare): `"Ended Mar 28, 2026"`
   - Neither: omit the element

Only `ThisSeasonView` passes `showExpandedHoverContent={true}`. Every existing caller (Discover, Advanced Search, Library, Sync, etc.) leaves the prop unset and sees the exact same card behavior as before. The expanded content is opt-in and scoped.

A small HTML-strip utility (`stripHtml(s)`) is needed to clean AniList descriptions. The implementation first searches the frontend codebase for an existing helper (other pages already render AniList descriptions — e.g., the entry page — so a helper may exist). If found, reuse it. If not, add a new helper next to other string utilities in `seanime-web/src/lib` (or `src/utils`, whichever is already used for this kind of thing) as a simple regex-based strip. Chosen during implementation based on what's there.

### `ThisSeasonView` page-layout integration

The existing schedule page file `seanime-web/src/app/(main)/schedule/page.tsx` is updated to:
- Wrap its content in the new `Tabs` component
- Keep the existing `MissingEpisodes` / `UpcomingEpisodes` / `ScheduleCalendar` layout inside the Calendar tab
- Render `ThisSeasonView` inside the This Season tab

---

## Data flow

### Backend — new endpoint

**Route**: `POST /api/v1/anilist/season-anime`

**Handler**: `HandleAnilistListSeasonAnime` in `internal/handlers/anilist.go`, registered next to the existing `HandleAnilistListAnime`.

**Request body**:
```go
type ListAnilistSeasonAnime_Variables struct {
    Season     string   `json:"season"`     // WINTER | SPRING | SUMMER | FALL
    SeasonYear int      `json:"seasonYear"`
    Sort       []string `json:"sort"`       // e.g. ["POPULARITY_DESC"]
}
```

**Response**: `[]*anilist.BaseAnime` (flat array, all pages concatenated).

**Logic**:
1. Bind the request body.
2. Resolve the per-profile adult flag via `h.getSettings(c)` — if `EnableAdultContent` is false, force the AniList query's `isAdult` arg to `false`. Same pattern `HandleAnilistListAnime` uses.
3. Loop the same AniList `ListAnime` platform method that `HandleAnilistListAnime` already calls (exact signature determined during implementation), starting at `page=1` with `perPage=50`, passing `season`, `seasonYear`, and `sort`. Append each page's media to a results slice, stopping when the returned `Page.PageInfo.hasNextPage` is `false`.
4. Respond with the flat slice.

The loop is sequential. AniList's rate limit is 90 requests per minute per IP; a typical season is 1–3 pages, worst-observed ~5 pages. No risk of tripping the limit for MVP use. A bounded parallel fan-out is a drop-in replacement if ever needed.

**Error handling**: if any page fetch fails, return the error immediately — the frontend shows the error card and lets the user retry. No partial results are returned.

**Route registration**: added to `internal/handlers/routes.go` next to the existing anilist routes.

**Codegen**: after the handler and request type are added, run the codegen tool (the same one used for any new endpoint) to regenerate `codegen/generated/*` and `seanime-web/src/api/generated/*`. This produces the frontend types automatically.

### Frontend — new hook

**File**: `seanime-web/src/api/hooks/anilist.hooks.ts`

**New hook**: `useAnilistListSeasonAnime(variables, enabled)` wrapping `useServerQuery<Array<AL_BaseAnime>>` with:
- `endpoint`: `API_ENDPOINTS.ANILIST.AnilistListSeasonAnime.endpoint`
- `method`: `POST`
- `queryKey`: `[API_ENDPOINTS.ANILIST.AnilistListSeasonAnime.key, variables.season, variables.seasonYear, variables.sort]`
- `data`: variables
- `enabled`: passed through

React Query caches per `(season, year, sort)` tuple. Switching controls produces a new cache entry automatically; switching back is instant.

### Data flow diagram

```
User changes season/sort
  |
  v
ThisSeasonView updates local state
  |
  v
computeSeasonParams(kind, now) -> {season, seasonYear}
  |
  v
useAnilistListSeasonAnime({season, seasonYear, sort}, enabled=true)
  |
  v
POST /api/v1/anilist/season-anime
  |
  v (handler loops AniList pages until exhausted)
  |
  v
[]*AL_BaseAnime (flat array)
  |
  v
MediaCardLazyGrid renders one MediaEntryCard per entry
  |
  v
User hovers a card -> hover popup shows synopsis + date range + score + add-to-list
User clicks a card -> /entry?id={mediaId}
```

---

## Error handling and edge cases

- **Rate limit**: sequential page fetches; a handful of pages per season stays well under AniList's 90 req/min limit.
- **Empty season**: aggregator returns an empty array (e.g., very early "next season" queries). UI shows an empty-state card.
- **Network failure**: the hook surfaces the error; the UI shows an error card with a Retry button. React Query's built-in retry (2 attempts) handles transient failures automatically.
- **Adult content**: backend forces `isAdult: false` on the AniList query when the per-profile setting is off. No adult results ever reach the frontend. `MediaEntryCard` already handles blur settings for any that do (mixed-adult edge cases from the server).
- **Rapid toggling**: React Query cancels in-flight fetches automatically when the query key changes, so Previous → Current → Next in quick succession doesn't leak requests.
- **Tab switching mid-fetch**: React Query keeps the cache entry warm, so leaving and returning to the tab shows the result immediately when it's ready.
- **Offline mode**: the existing `MediaEntryCard` generates `/offline/entry/anime?id={id}` links in offline mode. The season view itself still requires AniList (the aggregator calls AniList directly), so it's functional only when the user is online. In offline mode, the tab should either hide or show an "offline" empty state — MVP behavior: rendering the error card on fetch failure is acceptable.

---

## Testing

- **Manual smoke test (dev server)**:
  - Switch tabs between Calendar and This Season, verify Calendar stays default on reload.
  - Switch season between Previous / Current / Next, verify each shows a different list and the "Spring 2026" label updates.
  - Switch sort between Popularity / Trending / Score / Start date, verify ordering changes.
  - Hover a card and verify the synopsis + date range appear alongside the existing score and add-to-list button.
  - Click the add-to-list button, add a show to Planning, verify it syncs to AniList.
  - Click a card, verify it routes to `/entry?id={mediaId}`.
- **18+ badge test**:
  - Enable `EnableAdultContent` on the profile.
  - Verify adult shows appear in This Season and Calendar with the 18+ badge.
  - Verify the badge also appears on Discover and Advanced Search cards (shared infra side effect — should be confirmed, not a regression).
  - Disable `EnableAdultContent` and verify all adult entries disappear from the schedule page entirely (pre-existing behavior, no change).
- **Per-profile adult content test**: sign in as two profiles with different `EnableAdultContent` settings. Hit the endpoint from each. Confirm results differ — the disabled profile should never see adult results.
- **Large-season check**: pick a recent popular season (e.g. Fall 2024, which has 80+ anime entries across all formats). Confirm the aggregator returns all of them in a single response.
- **No unit tests**: the Go handler is a thin loop over an already-tested platform method with no branching logic worth isolating. The frontend component is composition over existing components. Both are covered by manual validation.

---

## Files touched

### Backend (new and modified)
- `internal/handlers/anilist.go` — new `HandleAnilistListSeasonAnime` handler
- `internal/handlers/routes.go` — register the new route

### Frontend (new and modified)
- `seanime-web/src/app/(main)/schedule/page.tsx` — wrap existing content in `Tabs`, add This Season tab
- `seanime-web/src/app/(main)/schedule/_components/this-season-view.tsx` — new `ThisSeasonView` component
- `seanime-web/src/app/(main)/schedule/_components/schedule-calendar.tsx` — add `isAdult` to `CalendarEvent`, populate it from `Anime_ScheduleItem`, render 18+ badge on desktop + mobile calendar entries
- `seanime-web/src/app/(main)/_features/media/_components/media-entry-card.tsx` — add optional `showExpandedHoverContent` prop (synopsis + date range), and render the 18+ badge unconditionally when `media.isAdult === true`
- `seanime-web/src/app/(main)/_features/media/_components/media-entry-card-components.tsx` — new `AdultContentBadge` (or similar) small badge component; or inlined depending on where similar overlays already live
- `seanime-web/src/api/hooks/anilist.hooks.ts` — new `useAnilistListSeasonAnime` hook
- `stripHtml` helper — reuse existing one if found, otherwise add to the appropriate `src/lib` or `src/utils` location (determined during implementation)

### Codegen output (regenerated, not hand-edited)
- `codegen/generated/handlers.json`
- `codegen/generated/public_structs.json`
- `seanime-web/src/api/generated/endpoints.ts`
- `seanime-web/src/api/generated/endpoint.types.ts`
- `seanime-web/src/api/generated/types.ts`
