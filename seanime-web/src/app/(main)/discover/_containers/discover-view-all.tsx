import { buildSeaQuery } from "@/api/client/requests"
import { AnilistListAnime_Variables, AnilistListManga_Variables } from "@/api/generated/endpoint.types"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { AL_BaseAnime, AL_BaseManga, AL_ListAnime, AL_ListManga } from "@/api/generated/types"
import { serverAuthTokenAtom } from "@/app/(main)/_atoms/server-status.atoms"
import { MediaCardGrid } from "@/app/(main)/_features/media/_components/media-card-grid"
import { MediaEntryCard } from "@/app/(main)/_features/media/_components/media-entry-card"
import { DiscoverAnimeList, discoverMangaVariables } from "@/app/(main)/discover/_lib/discover-variables"
import { __discover_trendingMangaGenresAtom, useDiscoverAnimeVariables } from "@/app/(main)/discover/_lib/handle-discover-queries"
import { Button } from "@/components/ui/button"
import { LoadingSpinner } from "@/components/ui/loading-spinner"
import { useInfiniteQuery } from "@tanstack/react-query"
import { useAtomValue } from "jotai/react"
import uniqBy from "lodash/uniqBy"
import { useInView } from "motion/react"
import React from "react"

const PAGE_SIZE = 48

export function DiscoverViewAllAnime({ list }: { list: DiscoverAnimeList }) {
    return <InfiniteMediaGrid type="anime" variables={useDiscoverAnimeVariables(list)} />
}

export function DiscoverViewAllManga({ country }: { country: string }) {
    const genres = useAtomValue(__discover_trendingMangaGenresAtom)
    return <InfiniteMediaGrid type="manga" variables={discoverMangaVariables(country, genres)} />
}

type InfiniteMediaGridProps =
    | { type: "anime", variables: AnilistListAnime_Variables }
    | { type: "manga", variables: AnilistListManga_Variables }

function InfiniteMediaGrid({ type, variables }: InfiniteMediaGridProps) {
    const password = useAtomValue(serverAuthTokenAtom)

    const { data, isError, isFetching, isFetchNextPageError, hasNextPage, isFetchingNextPage, fetchNextPage, refetch } = useInfiniteQuery({
        queryKey: ["discover-view-all", type, variables],
        initialPageParam: 1,
        queryFn: ({ pageParam }) => buildSeaQuery<AL_ListAnime | AL_ListManga>({
            endpoint: type === "anime" ? API_ENDPOINTS.ANILIST.AnilistListAnime.endpoint : API_ENDPOINTS.MANGA.AnilistListManga.endpoint,
            method: "POST",
            data: { ...variables, page: pageParam, perPage: PAGE_SIZE },
            password,
        }),
        getNextPageParam: (lastPage, _, lastPageParam) => lastPage?.Page?.pageInfo?.hasNextPage ? lastPageParam + 1 : undefined,
    })

    // The modal scrolls inside its own container, where an observer margin has no effect, so the
    // sentinel itself is tall: it enters the viewport once the user is within 600px of the end.
    const endRef = React.useRef<HTMLDivElement>(null)
    const nearEnd = useInView(endRef)
    React.useEffect(() => {
        if (nearEnd && hasNextPage && !isFetchingNextPage && !isFetchNextPageError) fetchNextPage()
    }, [nearEnd, hasNextPage, isFetchingNextPage, isFetchNextPageError])

    // Rankings can shift between page requests, so the same title may come back on two pages.
    const media = React.useMemo(() => uniqBy(data?.pages.flatMap(page => page?.Page?.media ?? []).filter(Boolean) ?? [], "id"), [data])

    if (!data) return isError && !isFetching ? <RetryMessage message="Couldn't load titles" onRetry={() => refetch()} /> : <LoadingSpinner />

    return (
        <>
            <MediaCardGrid maxCol={5}>
                {media.map(item => type === "anime"
                    ? <MediaEntryCard key={item.id} media={item as AL_BaseAnime} type="anime" showLibraryBadge showTrailer showPreviewButton />
                    : <MediaEntryCard key={item.id} media={item as AL_BaseManga} type="manga" showPreviewButton />)}
            </MediaCardGrid>
            <div className="relative">
                <div ref={endRef} className="absolute bottom-0 h-[600px] w-full pointer-events-none" />
            </div>
            {isFetchingNextPage && <LoadingSpinner />}
            {isFetchNextPageError && !isFetchingNextPage && <RetryMessage message="Couldn't load more titles" onRetry={() => fetchNextPage()} />}
        </>
    )
}

function RetryMessage({ message, onRetry }: { message: string, onRetry: () => void }) {
    return (
        <div className="flex flex-col items-center gap-2 py-6 text-[--muted]">
            <p>{message}</p>
            <Button intent="gray-subtle" size="sm" onClick={onRetry}>Try again</Button>
        </div>
    )
}
