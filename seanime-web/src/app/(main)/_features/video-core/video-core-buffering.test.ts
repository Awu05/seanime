import { describe, expect, it } from "vitest"
import { isVideoBuffering } from "./video-core-buffering"

const HAVE_CURRENT_DATA = 2
const HAVE_FUTURE_DATA = 3

describe("isVideoBuffering", () => {
    it("is buffering while playing without enough data", () => {
        expect(isVideoBuffering({ paused: false, ended: false, readyState: HAVE_CURRENT_DATA })).toBe(true)
    })

    it("is not buffering while paused, even without enough data", () => {
        // a stalled/waiting event while paused must not leave the spinner on
        expect(isVideoBuffering({ paused: true, ended: false, readyState: HAVE_CURRENT_DATA })).toBe(false)
    })

    it("is not buffering once enough data is loaded", () => {
        // resuming after the data finished loading while paused must clear the spinner
        expect(isVideoBuffering({ paused: false, ended: false, readyState: HAVE_FUTURE_DATA })).toBe(false)
    })

    it("is not buffering once playback has ended", () => {
        expect(isVideoBuffering({ paused: false, ended: true, readyState: HAVE_CURRENT_DATA })).toBe(false)
    })
})
