import { describe, expect, it } from "vitest"
import { getSkipChapters, getSkipPatternError, nextAutoSkip } from "./media-core-chapters"

function chapters(...labels: string[]) {
    return labels.map((label, index) => ({
        label,
        start: index * 60,
        end: (index + 1) * 60,
    }))
}

describe("chapter skipping", () => {
    it("keeps the first default opening and ending", () => {
        const list = chapters("Opening", "Episode", "Opening 2", "Credits")

        expect(getSkipChapters(list, "")).toEqual([list[0], list[3]])
    })

    it("keeps the existing intro chapter rule", () => {
        const list = chapters("Intro", "Episode", "Ending")

        expect(getSkipChapters(list, "")).toEqual([])
        expect(getSkipChapters(list, "", { guardIntro: false })).toEqual([list[2]])
    })

    it("adds all chapters matching custom regexes", () => {
        const list = chapters("Intro", "Episode", "Next Episode Preview", "Outro")

        expect(getSkipChapters(list, "^intro$,preview,^outro$")).toEqual([list[0], list[2], list[3]])
    })

    it("matches custom regexes case insensitively", () => {
        const list = chapters("PREVIEW")

        expect(getSkipChapters(list, "^preview$")).toEqual(list)
    })

    it("reports invalid regexes", () => {
        expect(getSkipPatternError("^Preview$,(")).toBe("Invalid regex: (")
        expect(getSkipPatternError("^Preview$")).toBe("")
    })
})

describe("auto skip", () => {
    const opening = { label: "Opening", start: 60, end: 150 }

    it("seeks past a chapter once while the browser still reports a position inside it", () => {
        const first = nextAutoSkip(opening, null)
        expect(first.seekTo).toBe(150)

        // TV Bro keeps reporting the old position until the seek finishes.
        const stillSeeking = nextAutoSkip(opening, first.lastSkippedEnd)
        expect(stillSeeking.seekTo).toBeNull()
        expect(stillSeeking.lastSkippedEnd).toBe(150)
    })

    it("skips the chapter again once playback has left it", () => {
        expect(nextAutoSkip(opening, null).seekTo).toBe(150)
    })

    it("still skips a different chapter right after another", () => {
        expect(nextAutoSkip({ label: "Ending", start: 1300, end: 1390 }, 150).seekTo).toBe(1390)
    })
})
