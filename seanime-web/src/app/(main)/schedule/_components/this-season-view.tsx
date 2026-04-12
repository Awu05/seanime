"use client"

import { AL_BaseAnime, AL_MediaFormat, AL_MediaSort, AL_MediaStatus } from "@/api/generated/types"
import { useAnilistListSeasonAnime } from "@/api/hooks/anilist.hooks"
import { MediaCardLazyGrid } from "@/app/(main)/_features/media/_components/media-card-grid"
import { MediaEntryCard } from "@/app/(main)/_features/media/_components/media-entry-card"
import { useMediaPreviewModal } from "@/app/(main)/_features/media/_containers/media-preview-modal"
import { computeSeasonParams, formatSeasonLabel, SeasonKind } from "@/app/(main)/schedule/_lib/season"
import { LuffyError } from "@/components/shared/luffy-error"
import { cn } from "@/components/ui/core/styling"
import { LoadingSpinner } from "@/components/ui/loading-spinner"
import { NativeSelect } from "@/components/ui/native-select"
import React from "react"

type SeasonKindOption = { value: SeasonKind; label: string }
const SEASON_KINDS: SeasonKindOption[] = [
    { value: "previous", label: "Previous" },
    { value: "current", label: "Current" },
    { value: "next", label: "Next" },
]

type SortOption = { value: AL_MediaSort; label: string }
const SORT_OPTIONS: SortOption[] = [
    { value: "POPULARITY_DESC", label: "Popularity" },
    { value: "TRENDING_DESC", label: "Trending" },
    { value: "SCORE_DESC", label: "Score" },
    { value: "START_DATE_DESC", label: "Start date" },
]

const FORMAT_OPTIONS: { value: AL_MediaFormat | "ALL"; label: string }[] = [
    { value: "ALL", label: "All formats" },
    { value: "TV", label: "TV" },
    { value: "TV_SHORT", label: "TV Short" },
    { value: "MOVIE", label: "Movie" },
    { value: "OVA", label: "OVA" },
    { value: "ONA", label: "ONA" },
    { value: "SPECIAL", label: "Special" },
    { value: "MUSIC", label: "Music" },
]

const STATUS_OPTIONS: { value: AL_MediaStatus | "ALL"; label: string }[] = [
    { value: "ALL", label: "All statuses" },
    { value: "RELEASING", label: "Releasing" },
    { value: "NOT_YET_RELEASED", label: "Not yet released" },
    { value: "FINISHED", label: "Finished" },
    { value: "CANCELLED", label: "Cancelled" },
    { value: "HIATUS", label: "Hiatus" },
]

const GENRE_LIST = [
    "Action", "Adventure", "Comedy", "Drama", "Ecchi", "Fantasy",
    "Horror", "Mahou Shoujo", "Mecha", "Music", "Mystery", "Psychological",
    "Romance", "Sci-Fi", "Slice of Life", "Sports", "Supernatural", "Thriller",
]

const GENRE_OPTIONS: { value: string; label: string }[] = [
    { value: "ALL", label: "All genres" },
    ...GENRE_LIST.map((g) => ({ value: g, label: g })),
]

function applyFilters(
    data: AL_BaseAnime[],
    format: AL_MediaFormat | "ALL",
    status: AL_MediaStatus | "ALL",
    genre: string,
): AL_BaseAnime[] {
    let result = data
    if (format !== "ALL") {
        result = result.filter((m) => m.format === format)
    }
    if (status !== "ALL") {
        result = result.filter((m) => m.status === status)
    }
    if (genre !== "ALL") {
        result = result.filter((m) => m.genres?.includes(genre))
    }
    return result
}

export function ThisSeasonView() {
    const [seasonKind, setSeasonKind] = React.useState<SeasonKind>("current")
    const [sort, setSort] = React.useState<AL_MediaSort>("POPULARITY_DESC")
    const [formatFilter, setFormatFilter] = React.useState<AL_MediaFormat | "ALL">("ALL")
    const [statusFilter, setStatusFilter] = React.useState<AL_MediaStatus | "ALL">("ALL")
    const [genreFilter, setGenreFilter] = React.useState<string>("ALL")

    const { season, seasonYear } = React.useMemo(
        () => computeSeasonParams(seasonKind),
        [seasonKind],
    )

    const { data, isLoading, isError, refetch } = useAnilistListSeasonAnime(
        { season, seasonYear, sort: [sort] },
        true,
    )

    const filteredData = React.useMemo(
        () => data ? applyFilters(data, formatFilter, statusFilter, genreFilter) : [],
        [data, formatFilter, statusFilter, genreFilter],
    )

    const { setPreviewModalMediaId } = useMediaPreviewModal()

    return (
        <div className="flex flex-col h-[calc(100vh-8rem)]" data-this-season-view>
            {/* Fixed header */}
            <div className="flex-none space-y-4">
                {/* Season label */}
                <h2 className="text-center text-2xl font-bold pt-4">
                    {formatSeasonLabel(season, seasonYear)}
                </h2>

                {/* Season switcher + sort */}
                <div className="flex items-center justify-center gap-3">
                    <div className="flex items-center rounded-lg border border-[--border]">
                        {SEASON_KINDS.map((opt, i) => (
                            <button
                                key={opt.value}
                                type="button"
                                className={cn(
                                    "px-5 py-1.5 text-sm font-medium transition-colors whitespace-nowrap",
                                    i === 0 && "rounded-l-lg",
                                    i === SEASON_KINDS.length - 1 && "rounded-r-lg",
                                    seasonKind === opt.value
                                        ? "bg-brand-500 text-white"
                                        : "bg-transparent text-[--muted] hover:text-white hover:bg-white/5",
                                )}
                                onClick={() => setSeasonKind(opt.value)}
                            >
                                {opt.label}
                            </button>
                        ))}
                    </div>

                    <NativeSelect
                        value={sort}
                        options={SORT_OPTIONS}
                        onChange={(e) => setSort(e.target.value as AL_MediaSort)}
                        className="w-36"
                        size="sm"
                    />
                </div>

                {/* Filters row */}
                <div className="flex items-center justify-center gap-3 pb-2">
                    <NativeSelect
                        value={formatFilter}
                        options={FORMAT_OPTIONS}
                        onChange={(e) => setFormatFilter(e.target.value as AL_MediaFormat | "ALL")}
                        className="w-36"
                        size="sm"
                    />

                    <NativeSelect
                        value={statusFilter}
                        options={STATUS_OPTIONS}
                        onChange={(e) => setStatusFilter(e.target.value as AL_MediaStatus | "ALL")}
                        className="w-44"
                        size="sm"
                    />

                    <NativeSelect
                        value={genreFilter}
                        options={GENRE_OPTIONS}
                        onChange={(e) => setGenreFilter(e.target.value)}
                        className="w-40"
                        size="sm"
                    />
                </div>
            </div>

            {/* Scrollable body */}
            <div className="flex-1 overflow-y-auto min-h-0 pr-2">
                {isLoading && (
                    <div className="flex items-center justify-center py-16">
                        <LoadingSpinner />
                    </div>
                )}

                {!isLoading && isError && (
                    <LuffyError title="Failed to load season anime" reset={() => refetch()}>
                        <p>Could not fetch anime for {formatSeasonLabel(season, seasonYear)}.</p>
                    </LuffyError>
                )}

                {!isLoading && !isError && (data?.length ?? 0) === 0 && (
                    <LuffyError title={null}>
                        <p>No anime found for {formatSeasonLabel(season, seasonYear)}</p>
                    </LuffyError>
                )}

                {!isLoading && !isError && (data?.length ?? 0) > 0 && filteredData.length === 0 && (
                    <div className="flex items-center justify-center py-16">
                        <p className="text-sm text-[--muted]">No anime match the selected filters.</p>
                    </div>
                )}

                {!isLoading && !isError && filteredData.length > 0 && (
                    <MediaCardLazyGrid itemCount={filteredData.length}>
                        {filteredData.map((media) => (
                            <MediaEntryCard
                                key={media.id}
                                media={media}
                                type="anime"
                                showLibraryBadge={true}
                                showExpandedHoverContent={true}
                                onClick={() => setPreviewModalMediaId(media.id, "anime")}
                            />
                        ))}
                    </MediaCardLazyGrid>
                )}
            </div>
        </div>
    )
}
