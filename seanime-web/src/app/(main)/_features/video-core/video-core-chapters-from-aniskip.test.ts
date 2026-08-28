import { describe, expect, it } from "vitest"
import { vc_createChaptersFromAniSkip } from "./video-core.utils"

describe("vc_createChaptersFromAniSkip", () => {
    it("creates opening and ending chapters when both are present", () => {
        const chapters = vc_createChaptersFromAniSkip({
            op: { interval: { startTime: 0, endTime: 90 } },
            ed: { interval: { startTime: 1300, endTime: 1400 } },
        }, 1420)

        expect(chapters.find(c => c.text === "Opening")).toMatchObject({ start: 0, end: 90 })
        expect(chapters.find(c => c.text === "Ending")).toMatchObject({ start: 1300, end: 1400 })
    })

    // Regression: AniSkip frequently has data for only one of op/ed (e.g. episode 1 has no
    // opening, or the final episode has no ending) - the seek bar should still show whichever
    // marker is available instead of discarding both.
    it("still creates an ending chapter when opening data is missing", () => {
        const chapters = vc_createChaptersFromAniSkip({
            op: null,
            ed: { interval: { startTime: 1300, endTime: 1400 } },
        }, 1420)

        expect(chapters.find(c => c.text === "Opening")).toBeUndefined()
        expect(chapters.find(c => c.text === "Ending")).toMatchObject({ start: 1300, end: 1400 })
    })

    it("still creates an opening chapter when ending data is missing", () => {
        const chapters = vc_createChaptersFromAniSkip({
            op: { interval: { startTime: 0, endTime: 90 } },
            ed: null,
        }, 1420)

        expect(chapters.find(c => c.text === "Opening")).toMatchObject({ start: 0, end: 90 })
        expect(chapters.find(c => c.text === "Ending")).toBeUndefined()
    })

    it("returns no chapters when neither is present", () => {
        const chapters = vc_createChaptersFromAniSkip({ op: null, ed: null }, 1420)
        expect(chapters).toEqual([])
    })
})
