import { useServerStatus } from "@/app/(main)/_hooks/use-server-status"

/**
 * Whether AniList is currently reachable, per the server's health check.
 *
 * Defaults to `true` when server status hasn't loaded yet, so the UI doesn't flash a
 * false-degraded state during initial page load.
 */
export function useAnilistHealthy() {
    const serverStatus = useServerStatus()
    return serverStatus?.anilistHealthy ?? true
}
