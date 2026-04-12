"use client"

import { AL_MediaSort } from "@/api/generated/types"
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

export function ThisSeasonView() {
    const [seasonKind, setSeasonKind] = React.useState<SeasonKind>("current")
    const [sort, setSort] = React.useState<AL_MediaSort>("POPULARITY_DESC")

    const { season, seasonYear } = React.useMemo(
        () => computeSeasonParams(seasonKind),
        [seasonKind],
    )

    const { data, isLoading, isError, refetch } = useAnilistListSeasonAnime(
        { season, seasonYear, sort: [sort] },
        true,
    )

    const { setPreviewModalMediaId } = useMediaPreviewModal()

    return (
        <div className="space-y-4" data-this-season-view>
            {/* Top control row */}
            <div className="flex flex-wrap items-center gap-3">
                {/* Season segmented control */}
                <div className="flex items-center rounded-lg overflow-hidden border border-[--border]">
                    {SEASON_KINDS.map((opt) => (
                        <button
                            key={opt.value}
                            type="button"
                            className={cn(
                                "px-3 py-1.5 text-sm font-medium transition-colors",
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

                {/* Sort dropdown */}
                <NativeSelect
                    value={sort}
                    options={SORT_OPTIONS}
                    onChange={(e) => setSort(e.target.value as AL_MediaSort)}
                    className="w-36"
                    size="sm"
                />

                {/* Resolved season label */}
                <span className="ml-auto text-sm font-medium text-[--muted]">
                    {formatSeasonLabel(season, seasonYear)}
                </span>
            </div>

            {/* Body */}
            {isLoading && (
                <LoadingSpinner />
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

            {!isLoading && !isError && (data?.length ?? 0) > 0 && (
                <MediaCardLazyGrid itemCount={data?.length ?? 0}>
                    {data?.map((media) => (
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
    )
}
