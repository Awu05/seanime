import { useAnilistListAnime } from "@/api/hooks/anilist.hooks"
import { DiscoverAnimeList, discoverAnimeVariables } from "@/app/(main)/discover/_lib/discover-variables"
import { atom } from "jotai"
import { useAtomValue } from "jotai/react"
import { useInView } from "motion/react"

export const __discover_trendingGenresAtom = atom<string[]>([])
export const __discover_currentSeasonGenresAtom = atom<string[]>([])
export const __discover_pastSeasonGenresAtom = atom<string[]>([])
export const __discover_trendingMangaGenresAtom = atom<string[]>([])

const ROW_PAGE = { page: 1, perPage: 20 }

const noGenresAtom = atom<string[]>([])
const GENRE_ATOMS: Partial<Record<DiscoverAnimeList, typeof noGenresAtom>> = {
    trending: __discover_trendingGenresAtom,
    thisSeason: __discover_currentSeasonGenresAtom,
    pastSeason: __discover_pastSeasonGenresAtom,
}

// The row's query including its selected genre, so "View all" lists what the row shows.
export function useDiscoverAnimeVariables(list: DiscoverAnimeList) {
    return discoverAnimeVariables(list, useAtomValue(GENRE_ATOMS[list] ?? noGenresAtom))
}

export function useDiscoverTrendingAnime() {
    return useAnilistListAnime({ ...ROW_PAGE, ...useDiscoverAnimeVariables("trending") }, true)
}

export function useDiscoverCurrentSeasonAnime(ref: any) {
    const isInView = useInView(ref, { once: true })
    return useAnilistListAnime({ ...ROW_PAGE, ...useDiscoverAnimeVariables("thisSeason") }, isInView)
}

export function useDiscoverPastSeasonAnime(ref: any) {
    const isInView = useInView(ref, { once: true })
    return useAnilistListAnime({ ...ROW_PAGE, ...useDiscoverAnimeVariables("pastSeason") }, isInView)
}

export function useDiscoverUpcomingAnime(ref: any) {
    const isInView = useInView(ref, { once: true })
    return useAnilistListAnime({ ...ROW_PAGE, ...useDiscoverAnimeVariables("upcoming") }, isInView)
}

export function useDiscoverPopularAnime(ref: any) {
    const isInView = useInView(ref, { once: true })
    return useAnilistListAnime({
        ...ROW_PAGE,
        sort: ["POPULARITY_DESC"],
    }, isInView)
}

export function useDiscoverTrendingMovies(ref: any) {
    const isInView = useInView(ref, { once: true })
    return useAnilistListAnime({ ...ROW_PAGE, ...useDiscoverAnimeVariables("trendingMovies") }, isInView)
}
