import { VideoCore_VideoPlaybackInfo } from "@/app/(main)/_features/video-core/video-core.atoms"
import { logger } from "@/lib/helpers/debug"
import { atom } from "jotai"

const log = logger("VIDEO CORE MEDIA SESSION")

export const vc_mediaSessionManager = atom<VideoCoreMediaSessionManager | null>(null)

const actions = ["play", "pause", "seekforward", "seekbackward", "seekto"] as const

const DEFAULT_SEEK_OFFSET = 10

// navigator.mediaSession holds one set of action handlers for the whole page, but several
// players can be mounted at once (the global native player and a page's own player). Active
// managers are kept in activation order and the last one owns the session. A manager that does
// not own it must not clear it, and when the owner deactivates, the session goes back to the most
// recent manager that is still active and still has its video on the page.
// Kept on globalThis so a dev hot reload of this module doesn't split the registry in two.
const activeManagers: VideoCoreMediaSessionManager[] =
    ((globalThis as any).__vcMediaSessionManagers ??= [])

export class VideoCoreMediaSessionManager {
    private video: HTMLVideoElement | null = null
    private playbackInfo: VideoCore_VideoPlaybackInfo | null = null
    private isActive = false
    private videoController: AbortController | null = null
    private eventTarget = new EventTarget()

    constructor() {
        if (!this.checkSupport()) return
    }

    setVideo(video: HTMLVideoElement | null) {
        if (video === this.video) return
        this.removeVideoListeners()
        this.video = video
        if (this.video && this.isActive) {
            this.setupVideoListeners()
            this.updatePlaybackState()
            this.updatePositionState()
        }
    }

    setPlaybackInfo(playbackInfo: VideoCore_VideoPlaybackInfo | null) {
        this.playbackInfo = playbackInfo
        if (this.isActive) {
            this.updateMetadata()
        }
    }

    activate() {
        if (!this.checkSupport() || this.isActive) return

        this.isActive = true
        activeManagers.push(this)
        this.claim()

        if (this.video) {
            this.setupVideoListeners()
        }
    }

    deactivate() {
        if (!this.isActive) return

        const wasOwner = this.isOwner()
        this.isActive = false
        removeManager(this)
        this.removeVideoListeners()

        if (!wasOwner) return

        const next = nextLiveManager()
        if (next) {
            next.claim()
        } else {
            this.onDisconnect()
        }
    }

    // Makes this player the owner of the session, e.g. when its video is put in Picture-in-Picture,
    // so the PiP window's controls act on the player that is shown.
    takeOwnership() {
        if (!this.isActive) return
        if (!this.isOwner()) {
            removeManager(this)
            activeManagers.push(this)
        }
        this.claim()
    }

    on(type: string, handler: EventListener) {
        this.eventTarget.addEventListener(type, handler)
    }

    off(type: string, handler: EventListener) {
        this.eventTarget.removeEventListener(type, handler)
    }

    destroy() {
        this.deactivate()
        this.video = null
        this.playbackInfo = null
    }

    /** @internal for nextLiveManager */
    hasLiveVideo(): boolean {
        return !!this.video && this.video.isConnected !== false
    }

    private checkSupport(): boolean {
        return "mediaSession" in navigator
    }

    private isOwner(): boolean {
        return this.isActive && activeManagers[activeManagers.length - 1] === this
    }

    // Takes the page's media session: registers this manager's handlers and publishes its
    // metadata, playback state and position.
    private claim() {
        if (!this.isActive) return
        this.setupActionHandlers()
        this.updateMetadata()
        this.updatePlaybackState()
        this.updatePositionState()
    }

    // The player that starts playing owns the session. If it already does, its handlers are
    // registered again in case something else cleared them; metadata is left alone.
    private handlePlay() {
        if (!this.isActive) return
        if (!this.isOwner()) {
            this.takeOwnership()
            return
        }
        this.setupActionHandlers()
        this.updatePlaybackState()
    }

    private dispatch(type: string, detail?: any) {
        this.eventTarget.dispatchEvent(new CustomEvent(type, { detail }))
    }

    private onDisconnect() {
        if (!("mediaSession" in navigator)) return

        navigator.mediaSession.playbackState = "none"

        for (const action of actions) {
            setActionHandlerSafe(action, null)
        }

        navigator.mediaSession.metadata = new MediaMetadata({
            title: "",
            artist: "",
            artwork: [],
        })

        setPositionStateSafe()
    }

    private updateMetadata() {
        if (!this.isOwner() || !("mediaSession" in navigator)) return

        const metadata = this.createMetadata()
        navigator.mediaSession.metadata = metadata
    }

    private updatePlaybackState() {
        if (!this.isOwner() || !("mediaSession" in navigator)) return

        navigator.mediaSession.playbackState = !this.video ? "none" : this.video.paused ? "paused" : "playing"
    }

    private updatePositionState() {
        if (!this.isOwner() || !("mediaSession" in navigator)) return

        if (!this.video) {
            setPositionStateSafe()
            return
        }

        const duration = this.video.duration
        if (!Number.isFinite(duration) || duration <= 0) {
            // unknown length (still loading, or a live stream): don't report a stale position
            setPositionStateSafe()
            return
        }

        setPositionStateSafe({
            duration,
            playbackRate: this.video.playbackRate || 1,
            position: Math.min(Math.max(this.video.currentTime || 0, 0), duration),
        })
    }

    private createMetadata(): MediaMetadata | null {
        if (!this.playbackInfo) return null

        const episode = this.playbackInfo.episode
        const anime = episode?.baseAnime

        const title = episode?.displayTitle || "Seanime"
        const artist = anime?.title?.userPreferred || anime?.title?.romaji || anime?.title?.english || "Video Player"

        const artwork: MediaImage[] = []
        const imageUrl = episode?.episodeMetadata?.image || anime?.coverImage?.large || anime?.coverImage?.medium

        if (imageUrl) {
            artwork.push({ src: imageUrl, sizes: "512x512", type: "image/webp" })
        }

        return new MediaMetadata({ title, artist, artwork })
    }

    private setupActionHandlers() {
        if (!("mediaSession" in navigator)) return

        const handleAction = this.handleAction.bind(this)
        for (const action of actions) {
            setActionHandlerSafe(action, handleAction)
        }
    }

    private handleAction(details: MediaSessionActionDetails) {
        switch (details.action) {
            case "play":
                this.dispatch("media-play-request")
                break
            case "pause":
                this.dispatch("media-pause-request")
                break
            case "seekto":
            case "seekforward":
            case "seekbackward":
                let seekTime: number
                if (details.seekTime !== undefined && details.seekTime !== null) {
                    // Chrome's Picture-in-Picture ±10s buttons send "seekto" with an absolute time
                    seekTime = details.seekTime
                } else {
                    // seekOffset is a positive number of seconds in both directions
                    const currentTime = this.video?.currentTime || 0
                    const offset = Math.abs(details.seekOffset ?? DEFAULT_SEEK_OFFSET)
                    seekTime = currentTime + (details.action === "seekbackward" ? -offset : offset)
                }
                this.dispatch("media-seek-request", { seekTime })
                break
        }
    }

    private setupVideoListeners() {
        if (!this.video) return

        this.removeVideoListeners()
        this.videoController = new AbortController()
        const options = { signal: this.videoController.signal }

        this.video.addEventListener("play", this.handlePlay.bind(this), options)
        this.video.addEventListener("pause", this.updatePlaybackState.bind(this), options)
        this.video.addEventListener("timeupdate", this.updatePositionState.bind(this), options)
        this.video.addEventListener("loadedmetadata", this.handleLoadedMetadata.bind(this), options)
    }

    private removeVideoListeners() {
        this.videoController?.abort()
        this.videoController = null
    }

    private handleLoadedMetadata() {
        this.updatePlaybackState()
        this.updatePositionState()
    }
}

function removeManager(manager: VideoCoreMediaSessionManager) {
    const idx = activeManagers.indexOf(manager)
    if (idx !== -1) activeManagers.splice(idx, 1)
}

// The most recent active manager whose video is still on the page. Managers whose player was
// unmounted without being deactivated are dropped rather than handed a dead session.
function nextLiveManager(): VideoCoreMediaSessionManager | null {
    for (let i = activeManagers.length - 1; i >= 0; i--) {
        const candidate = activeManagers[i]
        if (candidate.hasLiveVideo()) return candidate
        log.warning("Dropping media session manager whose video is no longer on the page")
        activeManagers.splice(i, 1)
    }
    return null
}

// Browsers may not support every action (e.g. "seekto" on older WebKit) and throw for it.
function setActionHandlerSafe(action: MediaSessionAction, handler: MediaSessionActionHandler | null) {
    try {
        navigator.mediaSession.setActionHandler(action, handler)
    }
    catch (err) {
        log.warning(`Media session action "${action}" is not supported`, err)
    }
}

function setPositionStateSafe(state?: MediaPositionState) {
    if (!("setPositionState" in navigator.mediaSession)) return
    try {
        navigator.mediaSession.setPositionState(state)
    }
    catch (err) {
        log.warning("Failed to set media session position state", err)
    }
}
