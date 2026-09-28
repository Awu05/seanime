import { AnilistListAnime_Variables, AnilistListManga_Variables } from "@/api/generated/endpoint.types"
import { computeSeasonParams } from "@/app/(main)/schedule/_lib/season"

// The AniList query behind each Discover anime row. Rows fetch the first page; "View all" pages
// through the same query, so both must build it here.
export type DiscoverAnimeList = "trending" | "thisSeason" | "pastSeason" | "upcoming" | "trendingMovies"

export function discoverAnimeVariables(list: DiscoverAnimeList, genres: string[], now = new Date()): AnilistListAnime_Variables {
    const genreFilter = genres.length > 0 ? genres : undefined
    switch (list) {
        case "trending":
            return { sort: ["TRENDING_DESC"], genres: genreFilter }
        case "thisSeason":
            return { sort: ["SCORE_DESC"], ...computeSeasonParams("current", now), genres: genreFilter }
        case "pastSeason":
            return { sort: ["SCORE_DESC"], ...computeSeasonParams("previous", now), genres: genreFilter }
        case "upcoming":
            return { sort: ["TRENDING_DESC"], status: ["NOT_YET_RELEASED"] }
        case "trendingMovies":
            return { sort: ["TRENDING_DESC"], format: "MOVIE", status: ["RELEASING", "FINISHED"] }
    }
}

export function discoverMangaVariables(country: string | undefined, genres: string[]): AnilistListManga_Variables {
    return { sort: ["TRENDING_DESC"], countryOfOrigin: country, genres: genres.length > 0 ? genres : undefined }
}
