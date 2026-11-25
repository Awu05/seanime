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

    it("treats an opening-length Intro as the opening when there is no Opening chapter", () => {
        // the chapter layout of a real release: cold open, 90s intro, episode, 90s credits, preview
        const list = [
            { label: "Episode", start: 0, end: 38 },
            { label: "Intro", start: 38, end: 128 },
            { label: "Episode", start: 128, end: 1315 },
            { label: "Credits", start: 1315, end: 1405 },
            { label: "Episode", start: 1405, end: 1420 },
        ]

        expect(getSkipChapters(list, "")).toEqual([list[1], list[3]])
    })

    it("doesn't skip an Intro that is too short to be an opening, but still skips the ending", () => {
        // a short "Intro" is usually a cold open with story in it
        const list = [
            { label: "Intro", start: 0, end: 30 },
            { label: "Episode", start: 30, end: 1300 },
            { label: "Ending", start: 1300, end: 1390 },
        ]

        expect(getSkipChapters(list, "")).toEqual([list[2]])
    })

    it("prefers an Opening chapter over an Intro", () => {
        const list = [
            { label: "Intro", start: 0, end: 90 },
            { label: "Opening", start: 90, end: 180 },
            { label: "Episode", start: 180, end: 1300 },
        ]

        expect(getSkipChapters(list, "")).toEqual([list[1]])
    })

    it("skips the first Intro without the length check when the guard is off", () => {
        const list = chapters("Intro", "Episode", "Ending")

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

    it("still skips a different chapter right after another", () => {
        expect(nextAutoSkip({ label: "Ending", start: 1300, end: 1390 }, 150).seekTo).toBe(1390)
    })
})
