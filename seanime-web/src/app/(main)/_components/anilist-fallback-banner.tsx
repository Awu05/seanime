import { useAnilistHealthy } from "@/app/(main)/_hooks/use-anilist-healthy"
import { Alert } from "@/components/ui/alert"

/**
 * Shown when AniList is unreachable and the server is falling back to SIMKL for
 * discover/search/schedule data. Renders nothing when AniList is healthy.
 */
export function AnilistFallbackBanner() {
    const anilistHealthy = useAnilistHealthy()

    if (anilistHealthy) return null

    return (
        <Alert
            intent="warning"
            title="AniList is currently unreachable"
            description="Showing limited results from SIMKL instead. Some filters and sections are unavailable until AniList recovers."
            className="mb-4"
        />
    )
}
