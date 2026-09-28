import { buildSeaQuery } from "@/api/client/requests"
import { AnilistListAnime_Variables, AnilistListManga_Variables } from "@/api/generated/endpoint.types"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { AL_ListAnime, AL_ListManga } from "@/api/generated/types"
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
// Matches the server's AniList list cache, so reopening a modal doesn't refetch every loaded page.
const STALE_TIME = 1000 * 60 * 10

export function DiscoverViewAllAnime({ list }: { list: DiscoverAnimeList }) {
    return <InfiniteMediaGrid
        endpoint={API_ENDPOINTS.ANILIST.AnilistListAnime.endpoint}
        variables={useDiscoverAnimeVariables(list)}
        getMedia={(page: AL_ListAnime | undefined) => page?.Page?.media}
        renderItem={media => <MediaEntryCard key={media.id} media={media} type="anime" showLibraryBadge showTrailer showPreviewButton />}
    />
}

export function DiscoverViewAllManga({ country }: { country: string }) {
    const genres = useAtomValue(__discover_trendingMangaGenresAtom)
    return <InfiniteMediaGrid
        endpoint={API_ENDPOINTS.MANGA.AnilistListManga.endpoint}
        variables={discoverMangaVariables(country, genres)}
        getMedia={(page: AL_ListManga | undefined) => page?.Page?.media}
        renderItem={media => <MediaEntryCard key={media.id} media={media} type="manga" showPreviewButton />}
    />
}

type InfiniteMediaGridProps<T extends AL_ListAnime | AL_ListManga, M extends { id: number }> = {
    endpoint: string
    variables: AnilistListAnime_Variables | AnilistListManga_Variables
    getMedia: (page: T | undefined) => M[] | undefined
    renderItem: (media: M) => React.ReactNode
}

function InfiniteMediaGrid<T extends AL_ListAnime | AL_ListManga, M extends { id: number }>(
    { endpoint, variables, getMedia, renderItem }: InfiniteMediaGridProps<T, M>,
) {
    const password = useAtomValue(serverAuthTokenAtom)

    const { data, isError, isFetching, isFetchNextPageError, hasNextPage, isFetchingNextPage, fetchNextPage, refetch } = useInfiniteQuery({
        queryKey: ["discover-view-all", endpoint, variables],
        initialPageParam: 1,
        queryFn: ({ pageParam }) => buildSeaQuery<T>({
            endpoint,
            method: "POST",
            data: { ...variables, page: pageParam, perPage: PAGE_SIZE },
            password,
        }),
        getNextPageParam: (lastPage, _, lastPageParam) => lastPage?.Page?.pageInfo?.hasNextPage ? lastPageParam + 1 : undefined,
        staleTime: STALE_TIME,
        gcTime: STALE_TIME,
    })

    // The modal scrolls inside its own container, where an observer margin has no effect, so the
    // sentinel itself is tall: it enters the viewport once the user is within 600px of the end.
    const endRef = React.useRef<HTMLDivElement>(null)
    const nearEnd = useInView(endRef)
    React.useEffect(() => {
        if (nearEnd && hasNextPage && !isFetchingNextPage && !isFetchNextPageError) fetchNextPage()
    }, [nearEnd, hasNextPage, isFetchingNextPage, isFetchNextPageError])

    // Rankings can shift between page requests, so the same title may come back on two pages.
    const media = React.useMemo(() => uniqBy(data?.pages.flatMap(page => getMedia(page) ?? []).filter(Boolean) ?? [], "id"), [data])

    if (!data) return isError && !isFetching ? <RetryMessage message="Couldn't load titles" onRetry={() => refetch()} /> : <LoadingSpinner />

    return (
        <>
            <MediaCardGrid maxCol={5}>
                {media.map(renderItem)}
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
