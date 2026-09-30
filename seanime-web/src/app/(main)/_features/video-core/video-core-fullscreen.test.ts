import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { VideoCoreFullscreenManager } from "./video-core-fullscreen"

// A document whose Fullscreen API never fires fullscreenchange, like TV Bro's Android WebView.
function silentFullscreenDocument(initiallyFullscreen: boolean) {
    const doc = new EventTarget() as EventTarget & Record<string, unknown>
    doc.fullscreenElement = initiallyFullscreen ? {} : null
    doc.exitFullscreen = async () => { doc.fullscreenElement = null }
    return doc
}

describe("VideoCoreFullscreenManager", () => {
    beforeEach(() => {
        vi.stubGlobal("navigator", { vendor: "Google Inc.", userAgent: "" })
        vi.stubGlobal("window", {})
    })

    afterEach(() => {
        vi.unstubAllGlobals()
    })

    it("reports the browser's real state when created, not a flag left over from an earlier player", () => {
        vi.stubGlobal("document", silentFullscreenDocument(false))
        const reported: boolean[] = []

        new VideoCoreFullscreenManager(isFullscreen => reported.push(isFullscreen))

        expect(reported).toEqual([false])
    })

    it("reports leaving fullscreen even when the browser fires no fullscreenchange event", async () => {
        vi.stubGlobal("document", silentFullscreenDocument(true))
        const reported: boolean[] = []
        const manager = new VideoCoreFullscreenManager(isFullscreen => reported.push(isFullscreen))

        await manager.exitFullscreen()

        expect(reported).toEqual([true, false])
    })
})
