import { describe, expect, it } from "vitest"
import { vc_getContinuitySavePayload } from "./video-core.utils"

function baseParams() {
    return {
        enableWatchContinuity: true,
        active: true,
        isWatchPartyParticipant: false,
        disableRestoreFromContinuity: false,
        mediaId: 21,
        episodeNumber: 5,
        isOnlinestream: false,
        currentTime: 120,
        duration: 1400,
    }
}

describe("vc_getContinuitySavePayload", () => {
    it("returns a save payload with kind=mediastream for a torrent/local stream", () => {
        expect(vc_getContinuitySavePayload(baseParams())).toEqual({
            mediaId: 21,
            episodeNumber: 5,
            currentTime: 120,
            duration: 1400,
            kind: "mediastream",
        })
    })

    it("returns kind=onlinestream when the session is an online stream", () => {
        const payload = vc_getContinuitySavePayload({ ...baseParams(), isOnlinestream: true })
        expect(payload?.kind).toBe("onlinestream")
    })

    it("returns null when watch continuity is disabled", () => {
        expect(vc_getContinuitySavePayload({ ...baseParams(), enableWatchContinuity: false })).toBeNull()
    })

    it("returns null when the player isn't active", () => {
        expect(vc_getContinuitySavePayload({ ...baseParams(), active: false })).toBeNull()
    })

    it("returns null for a watch-party participant (they shouldn't save the host's position as their own)", () => {
        expect(vc_getContinuitySavePayload({ ...baseParams(), isWatchPartyParticipant: true })).toBeNull()
    })

    it("returns null when restore-from-continuity is disabled for this session", () => {
        expect(vc_getContinuitySavePayload({ ...baseParams(), disableRestoreFromContinuity: true })).toBeNull()
    })

    it("returns null without a media id or episode number", () => {
        expect(vc_getContinuitySavePayload({ ...baseParams(), mediaId: null })).toBeNull()
        expect(vc_getContinuitySavePayload({ ...baseParams(), episodeNumber: null })).toBeNull()
    })

    it("returns null before playback has meaningfully started", () => {
        expect(vc_getContinuitySavePayload({ ...baseParams(), currentTime: 0 })).toBeNull()
    })

    it("returns null when duration is unknown (still buffering/live)", () => {
        expect(vc_getContinuitySavePayload({ ...baseParams(), duration: Infinity })).toBeNull()
        expect(vc_getContinuitySavePayload({ ...baseParams(), duration: 0 })).toBeNull()
    })
})
