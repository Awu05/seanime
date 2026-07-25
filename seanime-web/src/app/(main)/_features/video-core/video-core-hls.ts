import { vc_audioManager } from "@/app/(main)/_features/video-core/video-core"
import { getPreferredHlsQualityLevel } from "@/app/(main)/_features/video-core/_lib/hls-quality"
import { vc_autoPlayVideoAtom } from "@/app/(main)/_features/video-core/video-core.atoms"
import { logger } from "@/lib/helpers/debug"
import Hls, { ErrorData, Events, Level } from "hls.js"
import { atom, useAtomValue } from "jotai"
import { useAtom, useSetAtom } from "jotai/react"
import React, { useEffect, useRef } from "react"
import { toast } from "sonner"

export interface HlsQualityLevel {
    index: number
    height: number
    width: number
    bitrate: number
    name: string
}

export interface HlsAudioTrack {
    id: number
    name: string
    language?: string
    default?: boolean
}

export const vc_hlsQualityLevels = atom<HlsQualityLevel[]>([])
export const vc_hlsCurrentQuality = atom<number>(-1)
export const vc_hlsSetQuality = atom<((level: number) => void) | null>(null)
export const vc_hlsAudioTracks = atom<HlsAudioTrack[]>([])
export const vc_hlsCurrentAudioTrack = atom<number>(-1)
export const vc_hlsSetAudioTrack = atom<((trackId: number) => void) | null>(null)

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

const hlsLog = logger("VIDEO CORE HLS")
const MAX_MEDIA_ERROR_RECOVERY_ATTEMPTS = 2


export const HLS_VIDEO_EXTENSIONS = /\.(m3u8)($|\?)/i

export function isHLSSrc(src: string): boolean {
    return HLS_VIDEO_EXTENSIONS.test(src)
}

export const NATIVE_VIDEO_EXTENSIONS = /\.(mp4|avi|3gp|ogg)($|\?)/i

export function isNativeVideoExtension(src: string): boolean {
    return NATIVE_VIDEO_EXTENSIONS.test(src)
}

export function useVideoCoreHls({
    videoElement,
    streamUrl,
    streamType,
    preferredQuality,
    onFatalError,
    onStalled,
    onMediaDetached,
}: {
    videoElement: HTMLVideoElement | null
    streamUrl: string | undefined
    streamType?: string
    preferredQuality?: string
    onMediaDetached?: () => void
    onFatalError?: (error: ErrorData) => void
    onStalled?: (error: ErrorData) => void
}) {
    const hlsRef = useRef<Hls | null>(null)
    const hlsAutoPlayTriggered = useRef(false)
    const networkRecoveryAttempts = useRef(0)
    const preferredQualityRef = useRef(preferredQuality)

    useEffect(() => {
        preferredQualityRef.current = preferredQuality
    }, [preferredQuality])

    const audioManager = useAtomValue(vc_audioManager)
    const autoPlay = useAtomValue(vc_autoPlayVideoAtom)

    const [currentAudioTrack, setCurrentAudioTrack] = useAtom(vc_hlsCurrentAudioTrack)
    const setQualityLevels = useSetAtom(vc_hlsQualityLevels)
    const setCurrentQuality = useSetAtom(vc_hlsCurrentQuality)
    const setSetQuality = useSetAtom(vc_hlsSetQuality)
    const setAudioTracks = useSetAtom(vc_hlsAudioTracks)
    const setSetAudioTrack = useSetAtom(vc_hlsSetAudioTrack)

    useEffect(() => {
        if (!streamUrl || !videoElement) return

        const isHls = streamType === "hls" || isHLSSrc(streamUrl)

        if (!isHls) {
            hlsLog.info("Non-HLS stream, using native video element")
            // Cleanup HLS if it exists
            if (hlsRef.current) {
                hlsRef.current.destroy()
                hlsRef.current = null
            }
            setQualityLevels([])
            setCurrentQuality(-1)
            setSetQuality(() => {})
            setAudioTracks([])
            setCurrentAudioTrack(-1)
            setSetAudioTrack(() => {})
            return
        }

        if (Hls.isSupported()) {
            hlsLog.info("HLS.js supported, initializing HLS instance")

            // Destroy existing instance
            if (hlsRef.current) {
                hlsRef.current.destroy()
            }
            hlsAutoPlayTriggered.current = false
            networkRecoveryAttempts.current = 0

            // Extract clientId and the HMAC auth token from the stream URL to propagate to all
            // HLS sub-requests — HLS.js resolves segment/key URIs relative to the master playlist
            // URL, which drops the query string, so without this every segment request 401s when
            // a server password is set.
            let clientIdParam = ""
            let tokenParam = ""
            try {
                const urlObj = new URL(streamUrl, window.location.origin)
                const cid = urlObj.searchParams.get("clientId")
                if (cid) clientIdParam = cid
                const tok = urlObj.searchParams.get("token")
                if (tok) tokenParam = tok
            } catch {}

            // Create new HLS instance
            const hls = new Hls({
                enableWorker: true,
                lowLatencyMode: false,
                backBufferLength: 90,
                enableWebVTT: true,
                renderTextTracksNatively: false, // don't use native text tracks for subtitles
                maxBufferLength: 30, // Buffer up to 30 seconds ahead
                maxMaxBufferLength: 60,
                maxBufferHole: 2, // Tolerate up to 2 second gaps in buffer
                highBufferWatchdogPeriod: 3, // Wait 3 seconds before stall detection
                nudgeMaxRetry: 10, // More retries before giving up
                fragLoadPolicy: {
                    default: {
                        maxTimeToFirstByteMs: 30000,
                        maxLoadTimeMs: 120000,
                        timeoutRetry: { maxNumRetry: 5, retryDelayMs: 2000, maxRetryDelayMs: 16000 },
                        errorRetry: { maxNumRetry: 5, retryDelayMs: 2000, maxRetryDelayMs: 16000 },
                    },
                },
                // If the transcoder restarts or the network blips, give the manifest/playlist
                // load enough room to recover instead of failing fast and killing playback.
                manifestLoadPolicy: {
                    default: {
                        maxTimeToFirstByteMs: 20000,
                        maxLoadTimeMs: 60000,
                        timeoutRetry: { maxNumRetry: 5, retryDelayMs: 1000, maxRetryDelayMs: 8000 },
                        errorRetry: { maxNumRetry: 5, retryDelayMs: 1000, maxRetryDelayMs: 8000 },
                    },
                },
                playlistLoadPolicy: {
                    default: {
                        maxTimeToFirstByteMs: 20000,
                        maxLoadTimeMs: 60000,
                        timeoutRetry: { maxNumRetry: 5, retryDelayMs: 1000, maxRetryDelayMs: 8000 },
                        errorRetry: { maxNumRetry: 5, retryDelayMs: 1000, maxRetryDelayMs: 8000 },
                    },
                },
                // Propagate clientId and the auth token to all HLS sub-requests (index.m3u8, segments.ts)
                xhrSetup: (clientIdParam || tokenParam) ? (xhr, url) => {
                    let nextUrl = url
                    if (clientIdParam && !nextUrl.includes("clientId=")) {
                        const sep = nextUrl.includes("?") ? "&" : "?"
                        nextUrl = `${nextUrl}${sep}clientId=${clientIdParam}`
                    }
                    if (tokenParam && !nextUrl.includes("token=")) {
                        const sep = nextUrl.includes("?") ? "&" : "?"
                        nextUrl = `${nextUrl}${sep}token=${tokenParam}`
                    }
                    if (nextUrl !== url) {
                        xhr.open("GET", nextUrl, true)
                    }
                } : undefined,
            })
            let sourceLoaded = false
            let recoveringMediaError = false
            let mediaErrorRecoveryAttempts = 0
            let fatalErrorReported = false

            hlsRef.current = hls

            const reportFatalError = (data: ErrorData) => {
                if (fatalErrorReported) return
                fatalErrorReported = true

                hlsLog.error("Unrecoverable HLS error", data)
                if (hlsRef.current === hls) {
                    hlsRef.current = null
                }
                hls.destroy()
                onFatalError?.(data)
            }

            const recoverFatalMediaError = () => {
                if (mediaErrorRecoveryAttempts >= MAX_MEDIA_ERROR_RECOVERY_ATTEMPTS) {
                    return false
                }

                recoveringMediaError = true
                mediaErrorRecoveryAttempts += 1

                try {
                    if (mediaErrorRecoveryAttempts === MAX_MEDIA_ERROR_RECOVERY_ATTEMPTS) {
                        hlsLog.warning("Fatal media error, swapping audio codec and retrying")
                        hls.swapAudioCodec()
                    } else {
                        hlsLog.warning("Fatal media error, attempting recovery")
                    }

                    hls.recoverMediaError()
                    return true
                }
                catch (error) {
                    recoveringMediaError = false
                    hlsLog.error("Failed to recover from fatal media error", error)
                    return false
                }
            }

            // Quality setter function
            const qualitySetter = (levelIndex: number) => {
                if (!hls) return
                hlsLog.info("Setting quality level to", levelIndex)
                hls.currentLevel = levelIndex
                setCurrentQuality(levelIndex)
            }
            setSetQuality(() => qualitySetter)

            // Audio track setter function
            const audioTrackSetter = (trackId: number) => {
                if (!hls) return
                hlsLog.info("Setting audio track to", trackId)
                hls.audioTrack = trackId
                setCurrentAudioTrack(trackId)
            }
            setSetAudioTrack(() => audioTrackSetter)

            // Attach media element
            hls.attachMedia(videoElement)

            hls.on(Events.MEDIA_ATTACHED, () => {
                hlsLog.info("HLS media attached")
                if (!sourceLoaded) {
                    sourceLoaded = true
                    hls.loadSource(streamUrl)
                }
                recoveringMediaError = false
            })

            hls.on(Events.MEDIA_DETACHED, () => {
                hlsLog.info("HLS media detached")
                if (!recoveringMediaError) {
                    onMediaDetached?.()
                }
            })

            hls.on(Events.MANIFEST_PARSED, (event, data) => {
                hlsLog.info("HLS manifest parsed", data)

                // Extract quality levels
                const levels: HlsQualityLevel[] = data.levels.map((level: Level, index: number) => ({
                    index,
                    height: level.height,
                    width: level.width,
                    bitrate: level.bitrate,
                    name: level.height ? `${level.height}p` : `Level ${index + 1}`,
                }))

                setQualityLevels(levels)
                const preferredLevel = getPreferredHlsQualityLevel(levels, preferredQualityRef.current)
                if (preferredLevel !== null) {
                    hlsLog.info("Applying preferred quality level", preferredLevel)
                    hls.currentLevel = preferredLevel
                }
                setCurrentQuality(hls.currentLevel)

                // Extract audio tracks
                if (data.audioTracks && data.audioTracks.length > 0) {
                    hlsLog.info("Raw audio tracks from HLS", data.audioTracks)

                    // Deduplicate audio tracks
                    const uniqueTracks = new Map<string, { track: any, index: number }>()

                    data.audioTracks.forEach((track: any, index: number) => {
                        const key = `${track.id ?? index}-${track.groupId || ""}-${track.lang || "unknown"}-${track.name || ""}-${track.audioCodec || ""}`

                        // Keep the first occurrence of each unique track
                        if (!uniqueTracks.has(key)) {
                            uniqueTracks.set(key, { track, index })
                        }
                    })

                    const audioTracks: HlsAudioTrack[] = Array.from(uniqueTracks.values()).map(({ track, index }) => ({
                        id: typeof track.id === "number" ? track.id : index,
                        name: track.name || track.lang || `Track ${track.id}`,
                        language: track.lang,
                        default: track.default,
                    }))

                    hlsLog.info("Audio tracks", audioTracks)
                    setAudioTracks(audioTracks)
                    setCurrentAudioTrack(hls.audioTrack)
                } else {
                    setAudioTracks([])
                    setCurrentAudioTrack(-1)
                }

                // Defer autoplay until first fragment is buffered
                if (autoPlay && !hlsAutoPlayTriggered.current) {
                    hls.once(Events.FRAG_BUFFERED, () => {
                        if (!hlsAutoPlayTriggered.current) {
                            hlsAutoPlayTriggered.current = true
                            videoElement.play().catch(err => {
                                hlsLog.error("Failed to autoplay", err)
                            })
                        }
                    })
                }
            })

            // Reset recovery counters on each successful fragment load
            // so that transient network issues don't accumulate over a long session
            hls.on(Events.FRAG_LOADED, () => {
                if (networkRecoveryAttempts.current > 0) {
                    networkRecoveryAttempts.current = 0
                }
            })

            hls.on(Events.LEVEL_SWITCHED, (event, data) => {
                hlsLog.info("Quality level switched to", data.level)
                setCurrentQuality(hls.currentLevel)
            })

            hls.on(Events.AUDIO_TRACK_SWITCHED, (event, data) => {
                hlsLog.info("Audio track switched to", data.id)
                setCurrentAudioTrack(hls.audioTrack)
            })

            const MAX_NETWORK_RECOVERY_ATTEMPTS = 5

            hls.on(Events.FRAG_CHANGED, () => {
                if (mediaErrorRecoveryAttempts > 0) {
                    hlsLog.success("HLS media error recovery succeeded")
                    mediaErrorRecoveryAttempts = 0
                }
            })

            hls.on(Events.ERROR, (event, data: ErrorData) => {
                hlsLog.error("HLS error", data)
                if (data.details === Hls.ErrorDetails.BUFFER_STALLED_ERROR && !data.fatal) {
                    onStalled?.(data)
                }

                if (!data.fatal) return

                if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                    if (networkRecoveryAttempts.current < MAX_NETWORK_RECOVERY_ATTEMPTS) {
                        networkRecoveryAttempts.current++
                        hlsLog.warn(`Fatal network error, attempting recovery (${networkRecoveryAttempts.current}/${MAX_NETWORK_RECOVERY_ATTEMPTS})`)
                        hls.startLoad()
                        return
                    }
                }
                else if (data.type === Hls.ErrorTypes.MEDIA_ERROR && recoverFatalMediaError()) {
                    return
                }

                reportFatalError(data)
            })

            return () => {
                if (hlsRef.current) {
                    hlsLog.info("Destroying HLS instance")
                    hlsRef.current.destroy()
                    hlsRef.current = null
                }
            }
        } else if (videoElement.canPlayType("application/vnd.apple.mpegurl")) {
            hlsLog.info("Native support detected for HLS stream")
            videoElement.src = streamUrl
            setQualityLevels([])
            setCurrentQuality(-1)
            setSetQuality(() => {})
            setAudioTracks([])
            setCurrentAudioTrack(-1)
            setSetAudioTrack(() => {})
        } else {
            hlsLog.error("HLS not supported on this browser")
            toast.error("HLS playback not supported on this browser")
        }
    }, [streamUrl, videoElement, streamType])


    // Update audio manager when HLS audio track changes
    React.useEffect(() => {
        if (audioManager && currentAudioTrack !== -1) {
            audioManager.onHlsTrackChange?.(currentAudioTrack)
        }
    }, [currentAudioTrack, audioManager])
}

export const HLSMimeTypes = ["application/vnd.apple.mpegurl", "audio/mpegurl", "audio/x-mpegurl", "application/x-mpegurl", "video/x-mpegurl",
    "video/mpegurl", "application/mpegurl"]

export async function isProbablyHls(url: string): Promise<"hls" | "unknown"> {
    try {
        const controller = new AbortController()
        const timeoutId = setTimeout(() => controller.abort(), 5000)

        const response = await fetch(url, {
            method: "HEAD",
            cache: "no-store",
            signal: controller.signal,
        })

        clearTimeout(timeoutId)

        if (!response.ok) {
            console.warn(`Request for URL failed: ${response.status}`)
            return "unknown"
        }

        const contentType = response.headers.get("Content-Type")?.toLowerCase()

        if (contentType && HLSMimeTypes.includes(contentType)) {
            return "hls"
        }

        return "unknown"
    }
    catch (error) {
        if (error instanceof Error && error.name === "AbortError") {
            console.warn("Request timed out")
        } else {
            console.error("Error detecting stream type:", error)
        }
        return "unknown"
    }
}
