import { describe, expect, it } from "vitest"
import { wrapCaptionText } from "./video-core-caption-text-wrap"

// A fake measurer: width is just character count * 10, so wrapping decisions are predictable.
const measure = (s: string) => s.length * 10

describe("wrapCaptionText", () => {
    it("keeps a short line on one line", () => {
        expect(wrapCaptionText("hello world", 1000, measure)).toEqual(["hello world"])
    })

    it("wraps onto a new line once the max width would be exceeded", () => {
        // "hello world" is 110 wide (11 chars * 10), "hello" is 50 wide.
        expect(wrapCaptionText("hello world", 100, measure)).toEqual(["hello", "world"])
    })

    it("wraps multiple times for long text", () => {
        expect(wrapCaptionText("one two three four", 60, measure)).toEqual(["one", "two", "three", "four"])
    })

    it("keeps an overlong single word on its own line rather than dropping it", () => {
        expect(wrapCaptionText("supercalifragilisticexpialidocious", 10, measure)).toEqual(["supercalifragilisticexpialidocious"])
    })

    it("returns an empty array for empty text", () => {
        expect(wrapCaptionText("", 1000, measure)).toEqual([])
    })
})
