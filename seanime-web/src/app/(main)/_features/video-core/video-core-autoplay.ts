export type AutoplayVideoTarget = {
    muted: boolean
    play: () => Promise<void>
}

/**
 * Idempotently attempts to start playback (guarded by triggeredRef so multiple signals - e.g.
 * HLS.js's FRAG_BUFFERED plus the browser's own `canplay` event - can race to call this without
 * double-triggering). If the browser rejects an unmuted autoplay, retries muted, since browsers
 * generally allow muted autoplay even when they block audible autoplay.
 */
export function attemptHlsAutoplay(
    videoElement: AutoplayVideoTarget,
    triggeredRef: { current: boolean },
    onError?: (message: string, err: unknown) => void,
): void {
    if (triggeredRef.current) return
    triggeredRef.current = true

    videoElement.play().catch(err => {
        onError?.("Failed to autoplay, retrying muted", err)
        videoElement.muted = true
        videoElement.play().catch(mutedErr => {
            onError?.("Failed to autoplay even when muted", mutedErr)
        })
    })
}
