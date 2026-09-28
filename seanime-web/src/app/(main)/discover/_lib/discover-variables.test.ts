import { describe, expect, it } from "vitest"
import { discoverAnimeVariables, discoverMangaVariables, getPreviousSeason, getSeason } from "./discover-variables"

describe("getSeason", () => {
    it.each([
        [0, "WINTER"],
        [3, "SPRING"],
        [6, "SUMMER"],
        [9, "FALL"],
        [11, "FALL"],
    ] as const)("month index %i is %s", (month, season) => {
        expect(getSeason(new Date(2026, month, 15))).toEqual({ season, year: 2026 })
    })
})

describe("getPreviousSeason", () => {
    it("wraps winter back to the previous year's fall", () => {
        expect(getPreviousSeason(new Date(2026, 0, 15))).toEqual({ season: "FALL", year: 2025 })
    })

    it("stays in the same year otherwise", () => {
        expect(getPreviousSeason(new Date(2026, 4, 15))).toEqual({ season: "WINTER", year: 2026 })
    })
})

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
