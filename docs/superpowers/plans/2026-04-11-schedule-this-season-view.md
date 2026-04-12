# Schedule "This Season" View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "This Season" tab to the schedule page that shows AniList season listings (previous/current/next) with sort controls, synopsis + airing date range on card hover, and an 18+ badge on adult content across cards and calendar entries.

**Architecture:** One new backend aggregator endpoint (`POST /api/v1/anilist/season-anime`) paginates AniList season listings server-side and returns a flat array. A new React component `ThisSeasonView` wraps `MediaCardLazyGrid` with a three-way season switcher and a sort dropdown. `MediaEntryCard` gains one optional prop to render synopsis + date range in its hover popup, and unconditionally renders a new `AdultContentBadge` when `media.isAdult` is true. The schedule page is wrapped in a `Tabs` component with Calendar as the always-default tab (not persisted).

**Tech Stack:** Go (backend handler), React + TypeScript (frontend), React Query, Jotai, existing `MediaEntryCard` / `MediaCardLazyGrid` / `Tabs` components, existing `anilist.ListAnimeM` platform method.

**Design spec:** `docs/superpowers/specs/2026-04-11-schedule-this-season-view-design.md`

---

### Task 1: Backend handler — aggregator for season anime

**Files:**
- Modify: `internal/handlers/anilist.go`
- Modify: `internal/handlers/routes.go`

**Reference:** `HandleAnilistListAnime` in `internal/handlers/anilist.go` (lines ~268–350) is the template. It calls `anilist.ListAnimeM` with 13 positional args and applies per-profile adult filtering via `h.getSettings(c)`. Read it first to understand the call shape.

- [ ] **Step 1: Add the handler**

Append the following handler to `internal/handlers/anilist.go`, immediately after `HandleAnilistListAnime`:

```go
// HandleAnilistListSeasonAnime
//
//	@summary returns all anime in a given AniList season, aggregated across pages.
//	@desc Used by the schedule page's "This Season" tab. Loops AniList's ListAnime query
//	@desc server-side until the season is exhausted and returns a flat array of anime.
//	@desc Per-profile adult content filtering is applied: if the profile has
//	@desc EnableAdultContent=false, adult entries are excluded; if true, both adult and
//	@desc non-adult entries are returned (isAdult filter is omitted from the AniList query).
//	@route /api/v1/anilist/season-anime [POST]
//	@returns []anilist.BaseAnime
func (h *Handler) HandleAnilistListSeasonAnime(c echo.Context) error {

	type body struct {
		Season     *anilist.MediaSeason `json:"season"`
		SeasonYear *int                 `json:"seasonYear"`
		Sort       []*anilist.MediaSort `json:"sort"`
	}

	p := new(body)
	if err := c.Bind(p); err != nil {
		return h.RespondWithError(c, err)
	}

	if p.Season == nil || p.SeasonYear == nil {
		return h.RespondWithError(c, errors.New("season and seasonYear are required"))
	}

	// Per-profile adult content handling: if disabled, force isAdult=false so the AniList
	// query excludes adult entries. If enabled, pass nil so AniList returns both.
	enableAdult := false
	if currentSettings, settingsErr := h.getSettings(c); settingsErr == nil && currentSettings.GetAnilist() != nil {
		enableAdult = currentSettings.GetAnilist().EnableAdultContent
	}
	var isAdultPtr *bool
	if !enableAdult {
		falseVal := false
		isAdultPtr = &falseVal
	}

	perPage := 50
	results := make([]*anilist.BaseAnime, 0, 100)
	page := 1
	cacheLayer := shared_platform.NewCacheLayer(h.App.AnilistClientRef)
	for {
		ret, err := anilist.ListAnimeM(
			cacheLayer,
			&page,
			nil, // search
			&perPage,
			p.Sort,
			nil, // status
			nil, // genres
			nil, // averageScoreGreater
			p.Season,
			p.SeasonYear,
			nil, // format
			isAdultPtr,
			nil, // countryOfOrigin
			h.App.Logger,
			h.App.GetUserAnilistToken(),
		)
		if err != nil {
			return h.RespondWithError(c, err)
		}
		if ret == nil || ret.GetPage() == nil {
			break
		}

		media := ret.GetPage().GetMedia()
		if len(media) == 0 {
			break
		}
		results = append(results, media...)

		pageInfo := ret.GetPage().GetPageInfo()
		if pageInfo == nil || pageInfo.GetHasNextPage() == nil || !*pageInfo.GetHasNextPage() {
			break
		}

		page++
		// Safety cap: 20 * 50 = 1000 entries. No real AniList season exceeds this.
		if page > 20 {
			break
		}
	}

	return h.RespondWithData(c, results)
}
```

- [ ] **Step 2: Register the route**

Open `internal/handlers/routes.go`, find the line:

```go
v1Anilist.POST("/list-anime", h.HandleAnilistListAnime)
```

Add this line directly after it:

```go
v1Anilist.POST("/season-anime", h.HandleAnilistListSeasonAnime)
```

- [ ] **Step 3: Verify the backend builds**

Run: `go build ./internal/handlers/... ./internal/api/anilist/...`
Expected: clean build, no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/handlers/anilist.go internal/handlers/routes.go
git commit -m "feat: add aggregator endpoint for AniList season anime"
```

---

### Task 2: Regenerate codegen

**Files:**
- Regenerate: `codegen/generated/handlers.json`
- Regenerate: `codegen/generated/public_structs.json`
- Regenerate: `seanime-web/src/api/generated/endpoint.types.ts`
- Regenerate: `seanime-web/src/api/generated/endpoints.ts`
- Regenerate: `seanime-web/src/api/generated/types.ts`

- [ ] **Step 1: Run codegen**

```bash
cd codegen && go run main.go
```

Expected output: `Public structs extracted and saved to public_structs.json`

- [ ] **Step 2: Verify the new endpoint appears in generated files**

Run a grep:

```bash
grep -l "AnilistListSeasonAnime" seanime-web/src/api/generated/ codegen/generated/
```

Expected: matches in `endpoints.ts`, `endpoint.types.ts`, `handlers.json`. If zero matches, the handler's `@route` comment may have been missed — re-check Task 1 Step 1.

- [ ] **Step 3: Verify types are importable**

Run: `cd seanime-web && npx tsc --noEmit` (or the project's usual type-check command — if the repo has one configured, use that).

Expected: no new type errors introduced by the generated files.

- [ ] **Step 4: Commit**

```bash
git add codegen/generated/ seanime-web/src/api/generated/ internal/events/endpoints.go internal/extension_repo/goja_plugin_types/app.d.ts "seanime-web/src/app/(main)/_features/plugin/generated/plugin-events.ts"
git commit -m "chore: regenerate codegen for season-anime endpoint"
```

Note: some of the listed paths may not change — only stage files that actually show up in `git status`.

---

### Task 3: Frontend hook — `useAnilistListSeasonAnime`

**Files:**
- Modify: `seanime-web/src/api/hooks/anilist.hooks.ts`

**Reference:** `useAnilistListAnime` in the same file is the template.

- [ ] **Step 1: Add the hook**

Open `seanime-web/src/api/hooks/anilist.hooks.ts`. At the top, confirm the import block already destructures `AnilistListAnime_Variables` from `@/api/generated/endpoint.types`. Add `AnilistListSeasonAnime_Variables` to that same import.

Confirm the `AL_BaseAnime` import is present in the type imports. If not, add it.

Add this new hook at the end of the file (or immediately after `useAnilistListAnime`):

```ts
export function useAnilistListSeasonAnime(variables: AnilistListSeasonAnime_Variables, enabled: boolean) {
    return useServerQuery<Array<AL_BaseAnime>, AnilistListSeasonAnime_Variables>({
        endpoint: API_ENDPOINTS.ANILIST.AnilistListSeasonAnime.endpoint,
        method: API_ENDPOINTS.ANILIST.AnilistListSeasonAnime.methods[0],
        queryKey: [API_ENDPOINTS.ANILIST.AnilistListSeasonAnime.key, variables],
        data: variables,
        enabled: enabled,
        gcTime: 1000 * 60 * 10, // 10 minutes — match the backend cache TTL
    })
}
```

- [ ] **Step 2: Verify the hook compiles**

Run: `cd seanime-web && npx tsc --noEmit` (or the project's standard type-check command).

Expected: no errors. If `AL_BaseAnime` or `AnilistListSeasonAnime_Variables` is undefined, confirm Task 2 produced the regenerated types and that the imports at the top of `anilist.hooks.ts` include them.

- [ ] **Step 3: Commit**

```bash
git add seanime-web/src/api/hooks/anilist.hooks.ts
git commit -m "feat: add useAnilistListSeasonAnime hook"
```

---

### Task 4: Season helper + stripHtml utility

**Files:**
- Create (or modify existing): `seanime-web/src/app/(main)/schedule/_lib/season.ts`
- Possibly modify: wherever the existing `stripHtml` helper lives, or create a new small helper.

- [ ] **Step 1: Look for an existing HTML strip helper**

Run:

```bash
grep -rn "stripHtml\|stripHTML\|replace(/<" seanime-web/src/lib seanime-web/src/utils 2>&1
```

If an existing helper exists that strips HTML tags from a string, note its import path for use in Task 6. If none exists, proceed with Step 2 to create one.

- [ ] **Step 2: If no helper exists, create one**

If the previous grep found nothing suitable, create `seanime-web/src/lib/string-utils.ts` (or add to an existing utility file in `src/lib`):

```ts
/**
 * Strip HTML tags from a string and decode common entities.
 * AniList descriptions contain <br>, <i>, <b>, etc.
 */
export function stripHtml(html: string): string {
    if (!html) return ""
    // Remove tags
    let text = html.replace(/<[^>]*>/g, "")
    // Decode common entities
    text = text
        .replace(/&amp;/g, "&")
        .replace(/&lt;/g, "<")
        .replace(/&gt;/g, ">")
        .replace(/&quot;/g, "\"")
        .replace(/&#39;/g, "'")
        .replace(/&nbsp;/g, " ")
    return text.trim()
}
```

Note the import path for use in Task 6.

- [ ] **Step 3: Create the season helper**

Create `seanime-web/src/app/(main)/schedule/_lib/season.ts` with:

```ts
import { AL_MediaSeason } from "@/api/generated/types"

export type SeasonKind = "previous" | "current" | "next"

// Maps 0-indexed month to a base season.
// Jan(0)-Feb(1)-Mar(2) = WINTER
// Apr(3)-May(4)-Jun(5) = SPRING
// Jul(6)-Aug(7)-Sep(8) = SUMMER
// Oct(9)-Nov(10)-Dec(11) = FALL
function monthToSeasonIndex(month: number): number {
    return Math.floor(month / 3) // 0=WINTER, 1=SPRING, 2=SUMMER, 3=FALL
}

const SEASONS: AL_MediaSeason[] = ["WINTER", "SPRING", "SUMMER", "FALL"]

export function computeSeasonParams(
    kind: SeasonKind,
    now: Date = new Date(),
): { season: AL_MediaSeason; seasonYear: number } {
    const month = now.getMonth() // 0-11
    const year = now.getFullYear()

    let idx = monthToSeasonIndex(month)
    let y = year

    if (kind === "previous") {
        idx -= 1
        if (idx < 0) {
            idx = 3 // FALL
            y -= 1
        }
    } else if (kind === "next") {
        idx += 1
        if (idx > 3) {
            idx = 0 // WINTER
            y += 1
        }
    }

    return { season: SEASONS[idx], seasonYear: y }
}

export function formatSeasonLabel(season: AL_MediaSeason, year: number): string {
    const pretty: Record<AL_MediaSeason, string> = {
        WINTER: "Winter",
        SPRING: "Spring",
        SUMMER: "Summer",
        FALL: "Fall",
    }
    return `${pretty[season] ?? season} ${year}`
}
```

Check the `AL_MediaSeason` type path — if it's not under `@/api/generated/types`, adjust the import. Verify with:

```bash
grep -rn "AL_MediaSeason" seanime-web/src/api/generated/ | head -3
```

- [ ] **Step 4: Verify the helpers compile**

Run: `cd seanime-web && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add seanime-web/src/app/(main)/schedule/_lib/season.ts
# If stripHtml was created:
git add seanime-web/src/lib/string-utils.ts
git commit -m "feat: add season helper and stripHtml utility for This Season view"
```

---

### Task 5: `AdultContentBadge` component

**Files:**
- Modify: `seanime-web/src/app/(main)/_features/media/_components/media-entry-card-components.tsx`

**Reference:** The file already contains `MediaEntryCardOverlay` and various small exported helpers. Add the new badge component alongside them.

- [ ] **Step 1: Add the badge component**

Open `seanime-web/src/app/(main)/_features/media/_components/media-entry-card-components.tsx`. At the top of the file, confirm `Badge` is imported from `@/components/ui/badge`. If not, add the import.

Append this component at the end of the file:

```tsx
//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

/**
 * AdultContentBadge — small "18+" badge shown on media cards and calendar
 * entries when the underlying media has `isAdult === true`. The badge is
 * purely informational; adult filtering is enforced upstream by the
 * per-profile EnableAdultContent setting.
 */
export function AdultContentBadge() {
    return (
        <Badge
            intent="alert-solid"
            size="sm"
            className="font-bold tracking-wider leading-none h-5 px-1.5"
        >
            18+
        </Badge>
    )
}
```

If the project's `Badge` component uses a different `intent` value for red/warning, substitute accordingly (check `@/components/ui/badge` briefly to confirm — typical intents are `alert-solid`, `warning-solid`, or `error`). Pick whichever exists and produces a red/amber fill.

- [ ] **Step 2: Verify the component compiles**

Run: `cd seanime-web && npx tsc --noEmit`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add seanime-web/src/app/(main)/_features/media/_components/media-entry-card-components.tsx
git commit -m "feat: add AdultContentBadge component"
```

---

### Task 6: Render 18+ badge and synopsis/date range on `MediaEntryCard`

**Files:**
- Modify: `seanime-web/src/app/(main)/_features/media/_components/media-entry-card.tsx`

**Reference:** The hover popup body lives around lines 275–360. Existing badges (progress, score) are rendered inside `MediaEntryCardBody` as absolutely-positioned children around lines 399–437.

- [ ] **Step 1: Add the `showExpandedHoverContent` prop**

Open `seanime-web/src/app/(main)/_features/media/_components/media-entry-card.tsx`. Find the `MediaEntryCardProps` type (~line 60). Add the new prop inside the anime-only block (same place as `showTrailer`):

Change:

```ts
type MediaEntryCardProps<T extends "anime" | "manga"> = {
    type: T
    media: T extends "anime" ? AL_BaseAnime : T extends "manga" ? AL_BaseManga : never
    // Anime-only
    listData?: T extends "anime" ? Anime_EntryListData : T extends "manga" ? Manga_EntryListData : never
    showLibraryBadge?: T extends "anime" ? boolean : never
    showTrailer?: T extends "anime" ? boolean : never
```

To:

```ts
type MediaEntryCardProps<T extends "anime" | "manga"> = {
    type: T
    media: T extends "anime" ? AL_BaseAnime : T extends "manga" ? AL_BaseManga : never
    // Anime-only
    listData?: T extends "anime" ? Anime_EntryListData : T extends "manga" ? Manga_EntryListData : never
    showLibraryBadge?: T extends "anime" ? boolean : never
    showTrailer?: T extends "anime" ? boolean : never
    showExpandedHoverContent?: T extends "anime" ? boolean : never
```

- [ ] **Step 2: Destructure the new prop**

Find the destructuring block inside `MediaEntryCard` (~line 77–92). Add `showExpandedHoverContent` alongside the other props:

```ts
    const {
        media,
        listData: _listData,
        libraryData: _libraryData,
        nakamaLibraryData,
        overlay,
        showListDataButton,
        showTrailer: _showTrailer,
        showExpandedHoverContent = false,
        type,
        withAudienceScore = true,
        hideUnseenCountBadge = false,
        hideAnilistEntryEditButton = false,
        onClick,
        hideReleasingBadge = false,
    } = props
```

- [ ] **Step 3: Import dependencies for the expanded content**

At the top of `media-entry-card.tsx`, add these imports if they are not already present:

```ts
import { AdultContentBadge } from "@/app/(main)/_features/media/_components/media-entry-card-components"
import { stripHtml } from "@/lib/string-utils" // or wherever the helper lives, from Task 4
```

If the import block already destructures from `media-entry-card-components`, add `AdultContentBadge` to the existing import rather than creating a second line.

- [ ] **Step 4: Add a helper to format the airing date range**

Just above the `MediaEntryCard` function definition (around line 75), add:

```ts
function formatAiringDateRange(
    startDate?: { year?: number; month?: number; day?: number },
    endDate?: { year?: number; month?: number; day?: number },
): string | null {
    const fmt = (d?: { year?: number; month?: number; day?: number }) => {
        if (!d?.year) return null
        const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
        const parts: string[] = []
        if (d.month) parts.push(months[d.month - 1])
        if (d.day) parts.push(String(d.day))
        if (parts.length === 0) return String(d.year)
        return `${parts.join(" ")}, ${d.year}`
    }
    const start = fmt(startDate)
    const end = fmt(endDate)
    if (start && end) return `${start} – ${end}`
    if (start) return `Starts ${start}`
    if (end) return `Ended ${end}`
    return null
}
```

- [ ] **Step 5: Render synopsis + date range in the hover popup body**

Find the hover popup body section (~line 340, after `<AnimeEntryCardNextAiring ... />` and before the list-status paragraph). Insert the expanded content conditionally:

```tsx
                            {type === "anime" && (
                                <AnimeEntryCardNextAiring nextAiring={(media as AL_BaseAnime).nextAiringEpisode} />
                            )}

                            {showExpandedHoverContent && type === "anime" && (
                                <>
                                    {(() => {
                                        const range = formatAiringDateRange(
                                            (media as AL_BaseAnime).startDate,
                                            (media as AL_BaseAnime).endDate,
                                        )
                                        return range ? (
                                            <p className="text-center text-xs text-[--muted] w-full px-2">
                                                {range}
                                            </p>
                                        ) : null
                                    })()}
                                    {media.description && (
                                        <p className="text-xs text-[--muted] px-2 leading-snug line-clamp-4">
                                            {stripHtml(media.description)}
                                        </p>
                                    )}
                                </>
                            )}

                            {(listData?.status && listData?.status !== "CURRENT") &&
                                <p className="text-center text-xs text-[--muted] w-full">
                                    {capitalize(listData?.status ?? "")}
                                </p>}
```

- [ ] **Step 6: Render the 18+ badge on the card body**

Find the `MediaEntryCardBody` section (~line 381). Add a new absolute-positioned child for the adult badge alongside the existing progress/score badges. Place it inside the `<MediaEntryCardBody>` children, immediately after the opening block:

```tsx
            <MediaEntryCardBody
                link={link}
                type={type}
                /* ...existing props... */
            >
                {mediaIsAdult && (
                    <div data-media-entry-card-body-adult-badge-container className="absolute z-[11] right-1 top-1">
                        <AdultContentBadge />
                    </div>
                )}
                <div data-media-entry-card-body-progress-badge-container className="absolute z-[10] left-0 bottom-0 flex items-end">
                    {/* ...existing progress badge... */}
                </div>
```

Only add the new `{mediaIsAdult && ...}` block — do not touch the existing progress/score/missing-episode badge blocks.

- [ ] **Step 7: Verify build and check for type errors**

```bash
cd seanime-web && npx tsc --noEmit
```

Expected: no new errors.

- [ ] **Step 8: Commit**

```bash
git add seanime-web/src/app/(main)/_features/media/_components/media-entry-card.tsx
git commit -m "feat: MediaEntryCard supports expanded hover content and 18+ badge"
```

---

### Task 7: 18+ badge on calendar entries

**Files:**
- Modify: `seanime-web/src/app/(main)/schedule/_components/schedule-calendar.tsx`

**Reference:** The `CalendarEvent` type is declared ~line 279. The type is currently constructed from `Anime_ScheduleItem` objects wherever the calendar renders an entry.

- [ ] **Step 1: Add `isAdult` to the `CalendarEvent` type**

Find the `CalendarEvent` type (~line 279) and add `isAdult: boolean`:

```ts
type CalendarEvent = {
    id: string
    name: string
    time: string
    datetime: string
    href: string
    image: string
    episode: number
    isSeasonFinale: boolean
    isMovie: boolean
    isWatched: boolean
    isOnList: boolean
    isAdult: boolean
}
```

- [ ] **Step 2: Populate `isAdult` at every construction site**

Search for every place a `CalendarEvent`-shaped object is constructed in `schedule-calendar.tsx`:

```bash
grep -n "isSeasonFinale" seanime-web/src/app/(main)/schedule/_components/schedule-calendar.tsx
```

Every match identifies a place an event is built. At each site, add `isAdult: item.isAdult === true` (or `item.media?.isAdult === true`, depending on the source object — check the surrounding code). The source type should be `Anime_ScheduleItem`, which already has `isAdult: boolean`.

If the source object is not a `ScheduleItem` but an AniList airing schedule item, use `item?.media?.isAdult === true` instead.

- [ ] **Step 3: Import `AdultContentBadge`**

At the top of `schedule-calendar.tsx`, add:

```ts
import { AdultContentBadge } from "@/app/(main)/_features/media/_components/media-entry-card-components"
```

- [ ] **Step 4: Render the badge inline with the event title — desktop grid**

Find the desktop calendar event renderer (look for where `event.name` is rendered inside a `day.events.map` block). Insert the badge next to the title:

```tsx
<span className="flex items-center gap-1">
    {event.name}
    {event.isAdult && <AdultContentBadge />}
</span>
```

Replace the bare `{event.name}` reference with the above wrapper. If `event.name` is already inside a styled container, add the badge as a sibling inline-flex child instead of rewrapping.

- [ ] **Step 5: Render the badge in the mobile list**

Find `CalendarEventList` (~line 540) and its mobile event rendering. Apply the same change: next to where the event's title is shown, add `{event.isAdult && <AdultContentBadge />}` as a sibling.

- [ ] **Step 6: Verify build**

```bash
cd seanime-web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add seanime-web/src/app/(main)/schedule/_components/schedule-calendar.tsx
git commit -m "feat: show 18+ badge on calendar entries"
```

---

### Task 8: `ThisSeasonView` component

**Files:**
- Create: `seanime-web/src/app/(main)/schedule/_components/this-season-view.tsx`

**Reference:** The Discover page uses `MediaCardLazyGrid` with `MediaEntryCard` children in a similar composition. Look at `seanime-web/src/app/(main)/discover/_containers/*` for skeleton-loading / empty-state patterns if unsure.

- [ ] **Step 1: Create the component file**

Create `seanime-web/src/app/(main)/schedule/_components/this-season-view.tsx` with the following content:

```tsx
"use client"

import { AL_MediaSort } from "@/api/generated/types"
import { useAnilistListSeasonAnime } from "@/api/hooks/anilist.hooks"
import { MediaEntryCard } from "@/app/(main)/_features/media/_components/media-entry-card"
import { MediaCardLazyGrid } from "@/app/(main)/_features/media/_components/media-card-grid"
import { computeSeasonParams, formatSeasonLabel, SeasonKind } from "@/app/(main)/schedule/_lib/season"
import { Button } from "@/components/ui/button"
import { Select } from "@/components/ui/select"
import { LoadingSpinner } from "@/components/ui/loading-spinner"
import React from "react"

type SortOption = {
    value: AL_MediaSort
    label: string
}

const SORT_OPTIONS: SortOption[] = [
    { value: "POPULARITY_DESC", label: "Popularity" },
    { value: "TRENDING_DESC", label: "Trending" },
    { value: "SCORE_DESC", label: "Score" },
    { value: "START_DATE_DESC", label: "Start date" },
]

const SEASON_KINDS: { value: SeasonKind; label: string }[] = [
    { value: "previous", label: "Previous" },
    { value: "current", label: "Current" },
    { value: "next", label: "Next" },
]

export function ThisSeasonView() {
    const [seasonKind, setSeasonKind] = React.useState<SeasonKind>("current")
    const [sort, setSort] = React.useState<AL_MediaSort>("POPULARITY_DESC")

    const { season, seasonYear } = React.useMemo(
        () => computeSeasonParams(seasonKind),
        [seasonKind],
    )

    const { data, isLoading, isError, refetch } = useAnilistListSeasonAnime(
        {
            season,
            seasonYear,
            sort: [sort],
        },
        true,
    )

    return (
        <div className="space-y-4" data-this-season-view>
            {/* Top control row */}
            <div className="flex flex-col sm:flex-row sm:items-center gap-3">
                <div className="flex gap-1 rounded-[--radius] bg-[--background-muted] p-1" role="tablist" aria-label="Season">
                    {SEASON_KINDS.map(({ value, label }) => (
                        <Button
                            key={value}
                            size="sm"
                            intent={seasonKind === value ? "primary" : "gray-subtle"}
                            onClick={() => setSeasonKind(value)}
                            role="tab"
                            aria-selected={seasonKind === value}
                        >
                            {label}
                        </Button>
                    ))}
                </div>

                <Select
                    value={sort}
                    onValueChange={(v) => setSort(v as AL_MediaSort)}
                    options={SORT_OPTIONS.map(({ value, label }) => ({ value, label }))}
                    size="sm"
                    className="w-48"
                    aria-label="Sort by"
                />

                <div className="sm:ml-auto text-sm text-[--muted]">
                    {formatSeasonLabel(season, seasonYear)}
                </div>
            </div>

            {/* Body */}
            {isLoading && (
                <div className="flex items-center justify-center py-16">
                    <LoadingSpinner />
                </div>
            )}

            {isError && !isLoading && (
                <div className="rounded-[--radius] border border-[--border] bg-[--background-muted] p-6 text-center">
                    <p className="text-sm text-[--muted] mb-3">Failed to load season anime.</p>
                    <Button size="sm" intent="primary" onClick={() => refetch()}>
                        Retry
                    </Button>
                </div>
            )}

            {!isLoading && !isError && (data?.length ?? 0) === 0 && (
                <div className="rounded-[--radius] border border-[--border] bg-[--background-muted] p-6 text-center">
                    <p className="text-sm text-[--muted]">
                        No anime found for {formatSeasonLabel(season, seasonYear)}.
                    </p>
                </div>
            )}

            {!isLoading && !isError && (data?.length ?? 0) > 0 && (
                <MediaCardLazyGrid itemCount={data?.length ?? 0}>
                    {(data ?? []).map((media) => (
                        <MediaEntryCard
                            key={media.id}
                            type="anime"
                            media={media}
                            showLibraryBadge
                            showExpandedHoverContent
                        />
                    ))}
                </MediaCardLazyGrid>
            )}
        </div>
    )
}
```

**Before running type-check, verify these component APIs match the codebase:**

1. `Button` — verify the intent values and props (`size`, `intent`, `onClick`). If the project uses different names (e.g., `variant` instead of `intent`), adjust.
2. `Select` — the API above assumes a value/onValueChange/options shape. Check `@/components/ui/select` briefly. If it uses a different API (e.g., children-based `<SelectItem>`), adjust accordingly.
3. `LoadingSpinner` — verify the import path. If it's named differently (e.g., `Spinner`), rename the import.
4. `MediaCardLazyGrid` — verify its expected children shape. In particular, check whether it takes `itemCount` or infers from children. If it infers, drop the `itemCount` prop.

Run `grep -rn "export function Button\|export const Button" seanime-web/src/components/ui/button.tsx` and similar for each component to confirm.

- [ ] **Step 2: Verify it compiles**

```bash
cd seanime-web && npx tsc --noEmit
```

Fix any type errors that arise from mismatched component APIs discovered in Step 1.

- [ ] **Step 3: Commit**

```bash
git add "seanime-web/src/app/(main)/schedule/_components/this-season-view.tsx"
git commit -m "feat: add ThisSeasonView component"
```

---

### Task 9: Wire `Tabs` into schedule page

**Files:**
- Modify: `seanime-web/src/app/(main)/schedule/page.tsx`

**Reference:** The existing schedule page currently stacks `MissingEpisodes`, `UpcomingEpisodes`, `ScheduleCalendar`, and the `Show all airing` toggle vertically. After this task, all of that content lives inside a `Calendar` tab, and a sibling `This Season` tab renders the new `ThisSeasonView`.

- [ ] **Step 1: Read the current page**

Run:

```bash
cat "seanime-web/src/app/(main)/schedule/page.tsx"
```

Note: the exact JSX and imports. Preserve every existing element inside the Calendar tab.

- [ ] **Step 2: Wrap content in `Tabs`**

Add these imports at the top of `page.tsx` (if not already present):

```ts
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { ThisSeasonView } from "@/app/(main)/schedule/_components/this-season-view"
import React from "react"
```

Inside the main page component (the exported default), wrap the current page body in a `Tabs` component with two tabs. Keep the existing body (MissingEpisodes, UpcomingEpisodes, ScheduleCalendar, Show-all-airing toggle, plugin webview slots) inside the Calendar tab's `TabsContent`, and render `<ThisSeasonView />` inside the This Season tab's `TabsContent`.

A rough sketch (adapt to the actual existing structure — do not blindly copy):

```tsx
const [tab, setTab] = React.useState<string>("calendar")

return (
    <AppLayoutStack>
        <Tabs value={tab} onValueChange={setTab}>
            <TabsList>
                <TabsTrigger value="calendar">Calendar</TabsTrigger>
                <TabsTrigger value="this-season">This Season</TabsTrigger>
            </TabsList>
            <TabsContent value="calendar">
                {/* existing content moved here verbatim */}
            </TabsContent>
            <TabsContent value="this-season">
                <ThisSeasonView />
            </TabsContent>
        </Tabs>
    </AppLayoutStack>
)
```

**Do not** add `atomWithStorage` or any persistence — tab state is intentionally in-memory only, so reloads always land on Calendar.

Verify the project's `Tabs` component API by opening `@/components/ui/tabs` briefly. If it uses a different trigger/content composition (e.g., a single `<Tabs items=[...]>` prop API), adjust.

- [ ] **Step 3: Verify build**

```bash
cd seanime-web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add "seanime-web/src/app/(main)/schedule/page.tsx"
git commit -m "feat: wrap schedule page in Calendar / This Season tabs"
```

---

### Task 10: Full build + manual smoke test

**Files:** none modified unless bugs are found.

- [ ] **Step 1: Full backend build**

```bash
go build ./...
```

Expected: clean build, no errors.

- [ ] **Step 2: Full frontend type-check and lint**

```bash
cd seanime-web && npx tsc --noEmit
```

Expected: no errors introduced by this plan. Pre-existing errors outside the files touched are acceptable.

- [ ] **Step 3: Run the dev server and manually smoke-test**

Start seanime and log in. Navigate to the Schedule page.

Verify each of the following:

1. **Default tab**: Calendar tab is selected on page load. Reload the page and confirm Calendar is still the default (never This Season).
2. **Calendar tab contents**: `MissingEpisodes`, `UpcomingEpisodes`, `ScheduleCalendar`, and the `Show all airing` toggle all render exactly as before.
3. **This Season — switch tabs**: click "This Season" — the view loads with `Current` season and `Popularity` sort by default. A grid of anime cards appears. The resolved-season label (e.g., "Spring 2026") shows in the control row.
4. **Season switcher**: click "Previous" — the list changes to last season. Click "Next" — it switches to next season. The label updates each time. Cache hits on previously loaded seasons are instant.
5. **Sort dropdown**: switch between Popularity / Trending / Score / Start date. The list reorders after a brief fetch.
6. **Hover a card**: hover over a card on the This Season tab. The hover popup shows **synopsis** (line-clamped) and **airing date range** (e.g. "Jan 5 – Mar 28, 2026") in addition to the existing score, add-to-list button, and next-episode countdown.
7. **Click a card**: clicking the poster, title, or Watch button routes to `/entry?id={id}`.
8. **Add-to-list**: click the edit icon in the hover popup, set the show to Planning, save, and verify it appears on your AniList Planning list (check the AniList website to confirm round-trip works).
9. **18+ badge — adult enabled**:
   - In settings, toggle `Enable Adult Content` on and save.
   - Go back to Schedule → This Season → Current season.
   - If any adult anime are in the season, they should appear with a small red `18+` badge in the top-right corner of the card poster.
   - Hover an adult card and verify the existing `blurAdultContent` setting (if enabled) still blurs the poster as before.
   - Switch to the Calendar tab and verify adult show entries (if any are airing) also show the `18+` badge next to their title — both in the desktop grid and the mobile view (resize the window to test).
10. **18+ badge — adult disabled**:
    - Toggle `Enable Adult Content` off in settings.
    - Verify no adult shows appear on This Season or Calendar at all. The badge never needs to render in this state because the entries are filtered upstream.
11. **Full-season coverage**: switch to a known-large season (e.g., Fall 2024). Verify more than 50 anime load (the whole season, not just page 1). The grid virtualizes so this should scroll smoothly.
12. **Error state**: with the dev server running, stop the backend briefly and click a tab / sort change to force a fetch. Verify the error card with Retry button appears. Restart the backend, click Retry, verify recovery.
13. **Per-profile adult content (multi-user only)**: if multi-user is enabled, log in as two profiles with different `EnableAdultContent` settings. Verify the This Season listing differs — the disabled profile never sees adult results, the enabled one does.

- [ ] **Step 4: Fix any issues found**

If any step fails, iterate fixing + re-testing. Commit any fixes with descriptive messages (`fix: ...`).

- [ ] **Step 5: Final commit (if any fixes were needed)**

```bash
git status
# review any unstaged changes
git add <fixed files>
git commit -m "fix: smoke-test fixes for This Season view"
```

---

## Self-Review Coverage Map

Quick spec → task mapping so nothing is dropped:

| Spec requirement | Task(s) |
|---|---|
| New tab on schedule page, Calendar as default, not persisted | Task 9 |
| Previous / Current / Next season switcher | Task 8 |
| Popularity default + dropdown for Trending / Score / Start date | Task 8 |
| Full season coverage via server-side pagination | Task 1 |
| New backend endpoint `POST /api/v1/anilist/season-anime` | Tasks 1, 2 |
| Per-profile adult filter applied to aggregator | Task 1 |
| New `useAnilistListSeasonAnime` hook | Task 3 |
| `computeSeasonParams` helper | Task 4 |
| `stripHtml` helper (reuse or create) | Task 4 |
| `MediaEntryCard` synopsis + airing date range on hover | Task 6 |
| `AdultContentBadge` component | Task 5 |
| 18+ badge on `MediaEntryCard` when `media.isAdult` | Task 6 |
| 18+ badge on calendar desktop + mobile entries | Task 7 |
| `CalendarEvent.isAdult` field | Task 7 |
| Codegen regeneration | Task 2 |
| Loading / empty / error states | Task 8 |
| Manual smoke test (including 18+ badge scenarios) | Task 10 |
