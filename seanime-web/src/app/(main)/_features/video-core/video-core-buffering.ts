// HTMLMediaElement.HAVE_FUTURE_DATA
const HAVE_FUTURE_DATA = 3

/**
 * Whether the buffering spinner should show: the video is trying to play but doesn't have
 * enough data. This is derived from the element on every media event rather than set by
 * individual events, because events like "stalled" also fire while paused and nothing
 * guarantees a later event clears them (e.g. "canplay" won't fire again if the data finished
 * loading while paused).
 */
export function isVideoBuffering(video: Pick<HTMLVideoElement, "paused" | "ended" | "readyState">): boolean {
    return !video.paused && !video.ended && video.readyState < HAVE_FUTURE_DATA
}
