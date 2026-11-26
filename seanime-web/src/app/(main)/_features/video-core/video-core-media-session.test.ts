import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { VideoCoreMediaSessionManager } from "./video-core-media-session"
import { VideoCorePipManager } from "./video-core-pip"

type Handler = ((details: MediaSessionActionDetails) => void) | null

// navigator.mediaSession keeps one handler per action for the whole page.
function fakeMediaSession(unsupported: string[] = []) {
    const handlers = new Map<string, Handler>()
    const calls = { setActionHandler: 0, metadata: 0 }
    const positions: (MediaPositionState | undefined)[] = []
    let metadata: unknown = null
    return {
        handlers,
        calls,
        positions,
        playbackState: "none",
        get metadata() { return metadata },
        set metadata(value: unknown) {
            calls.metadata++
            metadata = value
        },
        setActionHandler(action: string, handler: Handler) {
            if (unsupported.includes(action)) throw new TypeError(`"${action}" is not a valid enum value`)
            calls.setActionHandler++
            handlers.set(action, handler)
        },
        setPositionState(state?: MediaPositionState) {
            positions.push(state)
        },
    }
}

function fakeVideo(currentTime = 100, opts: { paused?: boolean, duration?: number } = {}) {
    const video = new EventTarget() as EventTarget & Record<string, unknown>
    video.currentTime = currentTime
    video.duration = opts.duration ?? 1400
    video.paused = opts.paused ?? false
    video.playbackRate = 1
    video.isConnected = true
    return video as unknown as HTMLVideoElement & { isConnected: boolean }
}

let session: ReturnType<typeof fakeMediaSession>
let created: VideoCoreMediaSessionManager[] = []

function stubSession(unsupported: string[] = []) {
    session = fakeMediaSession(unsupported)
    vi.stubGlobal("navigator", { mediaSession: session })
}

function newManager(video: HTMLVideoElement) {
    const manager = new VideoCoreMediaSessionManager()
    created.push(manager)
    const seeks: number[] = []
    manager.on("media-seek-request", (e: Event) => seeks.push((e as CustomEvent).detail.seekTime))
    manager.setVideo(video)
    return { manager, seeks }
}

function press(action: string, details: Partial<MediaSessionActionDetails> = {}) {
    session.handlers.get(action)?.({ action, ...details } as MediaSessionActionDetails)
}

// Chrome's Picture-in-Picture ±10s buttons send "seekto" with an absolute time.
function pressPipSeek(seekTime: number) {
    press("seekto", { seekTime })
}

describe("VideoCoreMediaSessionManager", () => {
    beforeEach(() => {
        stubSession()
        vi.stubGlobal("MediaMetadata", class {
            constructor(public init: unknown) {}
        })
    })

    afterEach(() => {
        // active managers are tracked module-wide, so don't leak them into the next test
        created.forEach(m => m.destroy())
        created = []
        vi.unstubAllGlobals()
    })

    it("routes Chrome's PiP seekto to the player", () => {
        const { manager, seeks } = newManager(fakeVideo(100))
        manager.activate()

        pressPipSeek(110)

        expect(seeks).toEqual([110])
    })

    it("keeps an active player's handlers when another player is torn down", () => {
        // the global native player and a page's own player can both be mounted
        const page = newManager(fakeVideo(100))
        const other = newManager(fakeVideo(0))
        other.manager.activate()
        page.manager.activate()

        other.manager.destroy()
        pressPipSeek(110)

        expect(page.seeks).toEqual([110])
        expect(other.seeks).toEqual([])
    })

    it("hands the session back to a still-active player when the newer one deactivates", () => {
        const first = newManager(fakeVideo(100))
        const second = newManager(fakeVideo(0))
        first.manager.activate()
        second.manager.activate()

        second.manager.deactivate()
        pressPipSeek(110)

        expect(first.seeks).toEqual([110])
    })

    it("never hands the session to a player whose video left the page", () => {
        // a player unmounted without being deactivated, e.g. before unmount cleanup existed
        const deadVideo = fakeVideo(0)
        const dead = newManager(deadVideo)
        const live = newManager(fakeVideo(100))
        dead.manager.activate()
        live.manager.activate()
        deadVideo.isConnected = false

        live.manager.deactivate()
        pressPipSeek(50)

        expect(dead.seeks).toEqual([])
        expect(session.handlers.get("seekto")).toBeNull()
        expect(session.playbackState).toBe("none")
    })

    it("publishes the new owner's position when the session is handed back", () => {
        const first = newManager(fakeVideo(120, { paused: true }))
        const second = newManager(fakeVideo(1390))
        first.manager.activate()
        second.manager.activate()

        second.manager.deactivate()

        expect(session.positions.at(-1)).toEqual({ duration: 1400, playbackRate: 1, position: 120 })
        expect(session.playbackState).toBe("paused")
    })

    it("clears the handlers when the last active player deactivates", () => {
        const { manager } = newManager(fakeVideo(100))
        manager.activate()

        manager.deactivate()

        expect(session.handlers.get("seekto")).toBeNull()
    })

    it("restores its handlers when its video starts playing", () => {
        const video = fakeVideo(100)
        const { manager, seeks } = newManager(video)
        manager.activate()
        session.setActionHandler("seekto", null) // cleared by something else

        video.dispatchEvent(new Event("play"))
        pressPipSeek(110)

        expect(seeks).toEqual([110])
    })

    it("the player that takes ownership (e.g. put in PiP) gets the controls", () => {
        const owner = newManager(fakeVideo(100))
        const shownInPip = newManager(fakeVideo(500, { paused: true }))
        shownInPip.manager.activate()
        owner.manager.activate()

        shownInPip.manager.takeOwnership()
        pressPipSeek(510)

        expect(shownInPip.seeks).toEqual([510])
        expect(owner.seeks).toEqual([])
    })

    it("does not take the session back after it has been deactivated", () => {
        const video = fakeVideo(100)
        const { manager } = newManager(video)
        manager.activate()
        manager.deactivate()

        video.dispatchEvent(new Event("play"))
        manager.takeOwnership()

        expect(session.handlers.get("seekto")).toBeNull()
    })

    it("seeks backward by a positive seekOffset", () => {
        const { manager, seeks } = newManager(fakeVideo(100))
        manager.activate()

        press("seekbackward", { seekOffset: 10 })
        press("seekforward", { seekOffset: 5 })
        press("seekbackward")

        expect(seeks).toEqual([90, 105, 90])
    })

    it("does not add listeners again when given the same video", () => {
        const video = fakeVideo(100)
        const { manager } = newManager(video)
        manager.activate()
        session.setActionHandler("seekto", null)
        manager.setVideo(video)
        manager.setVideo(video)
        const before = session.calls.setActionHandler

        video.dispatchEvent(new Event("play"))

        expect(session.calls.setActionHandler - before).toBe(5) // one registration per action
    })

    it("does not rebuild metadata on every resume while it already owns the session", () => {
        const video = fakeVideo(100)
        const { manager } = newManager(video)
        manager.activate()
        const before = session.calls.metadata

        video.dispatchEvent(new Event("play"))
        video.dispatchEvent(new Event("play"))

        expect(session.calls.metadata).toBe(before)
    })

    it("still works when the browser rejects an action it doesn't support", () => {
        stubSession(["seekto"])
        const { manager, seeks } = newManager(fakeVideo(100))
        manager.activate()

        press("seekforward")
        expect(seeks).toEqual([110])

        expect(() => manager.deactivate()).not.toThrow()
    })
})

describe("VideoCorePipManager.ownsPipElement", () => {
    beforeEach(() => {
        vi.stubGlobal("document", new EventTarget())
    })

    afterEach(() => {
        vi.unstubAllGlobals()
    })

    it("recognizes its own video and its subtitle canvas proxy, not another player's video", () => {
        const pip = new VideoCorePipManager(() => {})
        const video = fakeVideo(0)
        pip.setVideo(video, null as any)
        const proxy = fakeVideo(0)
        ;(pip as any).pipProxy = proxy

        expect(pip.ownsPipElement(video)).toBe(true)
        expect(pip.ownsPipElement(proxy as any)).toBe(true)
        expect(pip.ownsPipElement(fakeVideo(0) as any)).toBe(false)
        expect(pip.ownsPipElement(null)).toBe(false)
    })
})
