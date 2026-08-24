export type NativePlayerLifecycleEventType = "open-and-await" | "watch" | "abort-open"

/**
 * Whether a server lifecycle event should be applied to the player state.
 *
 * The backend has no request/correlation id for these events, so a "watch" or "abort-open"
 * that was already in flight when the user closed the player (handleTerminateStream) can still
 * arrive afterward. Applying it would resurrect a stream the user already closed. "open-and-await"
 * always applies - it's the signal that a new stream lifecycle is starting, and re-arms the
 * guard for the events that follow it.
 */
export function shouldApplyNativePlayerEvent(eventType: NativePlayerLifecycleEventType, terminated: boolean): boolean {
    if (eventType === "open-and-await") return true
    return !terminated
}
