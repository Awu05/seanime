import { AnilistListAnime_Variables, AnilistListManga_Variables } from "@/api/generated/endpoint.types"
import { AL_MediaSeason } from "@/api/generated/types"

// The AniList query behind each Discover anime row. Rows fetch the first page; "View all" pages
// through the same query, so both must build it here.
export type DiscoverAnimeList = "trending" | "thisSeason" | "pastSeason" | "upcoming" | "trendingMovies"

const SEASONS: AL_MediaSeason[] = ["WINTER", "SPRING", "SUMMER", "FALL"]

export function getSeason(date: Date): { season: AL_MediaSeason, year: number } {
    return { season: SEASONS[Math.floor(date.getMonth() / 3)], year: date.getFullYear() }
}

export function getPreviousSeason(date: Date): { season: AL_MediaSeason, year: number } {
    const { season, year } = getSeason(date)
    const index = SEASONS.indexOf(season)
    return index === 0 ? { season: "FALL", year: year - 1 } : { season: SEASONS[index - 1], year }
}

export function discoverAnimeVariables(list: DiscoverAnimeList, genres: string[], now = new Date()): AnilistListAnime_Variables {
    const genreFilter = genres.length > 0 ? genres : undefined
    switch (list) {
        case "trending":
            return { sort: ["TRENDING_DESC"], genres: genreFilter }
        case "thisSeason": {
            const { season, year } = getSeason(now)
            return { sort: ["SCORE_DESC"], season, seasonYear: year, genres: genreFilter }
        }
        case "pastSeason": {
            const { season, year } = getPreviousSeason(now)
            return { sort: ["SCORE_DESC"], season, seasonYear: year, genres: genreFilter }
        }
        case "upcoming":
            return { sort: ["TRENDING_DESC"], status: ["NOT_YET_RELEASED"] }
        case "trendingMovies":
            return { sort: ["TRENDING_DESC"], format: "MOVIE", status: ["RELEASING", "FINISHED"] }
    }
}

export function discoverMangaVariables(country: string | undefined, genres: string[]): AnilistListManga_Variables {
    return { sort: ["TRENDING_DESC"], countryOfOrigin: country, genres: genres.length > 0 ? genres : undefined }
}
