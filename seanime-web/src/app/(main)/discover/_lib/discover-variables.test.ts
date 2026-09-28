import { describe, expect, it } from "vitest"
import { discoverAnimeVariables, discoverMangaVariables } from "./discover-variables"

describe("discoverAnimeVariables", () => {
    const summer = new Date(2026, 7, 1)

    it("filters trending by the selected genre", () => {
        expect(discoverAnimeVariables("trending", ["Action"], summer)).toEqual({ sort: ["TRENDING_DESC"], genres: ["Action"] })
    })

    it("sends no genre filter when none is selected", () => {
        expect(discoverAnimeVariables("trending", [], summer).genres).toBeUndefined()
    })

    it("ranks this season by score", () => {
        expect(discoverAnimeVariables("thisSeason", [], summer)).toEqual({ sort: ["SCORE_DESC"], season: "SUMMER", seasonYear: 2026 })
    })

    it("ranks last season by score", () => {
        expect(discoverAnimeVariables("pastSeason", [], summer)).toEqual({ sort: ["SCORE_DESC"], season: "SPRING", seasonYear: 2026 })
    })

    it("wraps last season in January back to the previous year's fall", () => {
        expect(discoverAnimeVariables("pastSeason", [], new Date(2026, 0, 15))).toMatchObject({ season: "FALL", seasonYear: 2025 })
    })

    it("uses the current year for this season in December", () => {
        expect(discoverAnimeVariables("thisSeason", [], new Date(2026, 11, 20))).toMatchObject({ season: "FALL", seasonYear: 2026 })
    })

    it("lists upcoming titles by trend", () => {
        expect(discoverAnimeVariables("upcoming", [], summer)).toEqual({ sort: ["TRENDING_DESC"], status: ["NOT_YET_RELEASED"] })
    })

    it("lists released movies by trend", () => {
        expect(discoverAnimeVariables("trendingMovies", [], summer)).toEqual({
            sort: ["TRENDING_DESC"],
            format: "MOVIE",
            status: ["RELEASING", "FINISHED"],
        })
    })
})

describe("discoverMangaVariables", () => {
    it("filters trending manga by country and genre", () => {
        expect(discoverMangaVariables("KR", ["Drama"])).toEqual({ sort: ["TRENDING_DESC"], countryOfOrigin: "KR", genres: ["Drama"] })
    })

    it("omits empty filters", () => {
        const variables = discoverMangaVariables(undefined, [])
        expect(variables.countryOfOrigin).toBeUndefined()
        expect(variables.genres).toBeUndefined()
    })
})
