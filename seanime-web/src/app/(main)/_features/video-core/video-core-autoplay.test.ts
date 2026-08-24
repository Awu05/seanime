import { describe, expect, it, vi } from "vitest"
import { attemptHlsAutoplay } from "./video-core-autoplay"

function makeVideoElement(playImpl: () => Promise<void>) {
    return {
        muted: false,
        play: vi.fn(playImpl),
    }
}

describe("attemptHlsAutoplay", () => {
    it("plays the video and marks the ref as triggered", async () => {
        const video = makeVideoElement(() => Promise.resolve())
        const triggeredRef = { current: false }

        attemptHlsAutoplay(video, triggeredRef)
        await Promise.resolve()

        expect(video.play).toHaveBeenCalledTimes(1)
        expect(triggeredRef.current).toBe(true)
    })

    it("does nothing if already triggered, even if called again", async () => {
        const video = makeVideoElement(() => Promise.resolve())
        const triggeredRef = { current: true }

        attemptHlsAutoplay(video, triggeredRef)
        await Promise.resolve()

        expect(video.play).not.toHaveBeenCalled()
    })

    it("retries muted when the initial unmuted play() is rejected by the browser", async () => {
        let calls = 0
        const video = makeVideoElement(() => {
            calls++
            return calls === 1 ? Promise.reject(new Error("NotAllowedError")) : Promise.resolve()
        })
        const triggeredRef = { current: false }

        attemptHlsAutoplay(video, triggeredRef)
        await Promise.resolve()
        await Promise.resolve()
        await Promise.resolve()

        expect(video.play).toHaveBeenCalledTimes(2)
        expect(video.muted).toBe(true)
    })

    it("reports an error instead of throwing when even the muted retry is rejected", async () => {
        const video = makeVideoElement(() => Promise.reject(new Error("still blocked")))
        const triggeredRef = { current: false }
        const onError = vi.fn()

        expect(() => attemptHlsAutoplay(video, triggeredRef, onError)).not.toThrow()
        await Promise.resolve()
        await Promise.resolve()
        await Promise.resolve()

        expect(onError).toHaveBeenCalledTimes(2)
    })
})
