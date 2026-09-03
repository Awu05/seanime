import { useServerMutation, useServerQuery } from "@/api/client/requests"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { SeaLink } from "@/components/shared/sea-link"
import { Switch } from "@/components/ui/switch"
import { useQueryClient } from "@tanstack/react-query"
import React from "react"
import { LuCircleCheck, LuLoaderCircle } from "react-icons/lu"
import { toast } from "sonner"

type SimklSettings = {
    enabled: boolean
    connected: boolean
    clientId: string
}

type SimklSyncStatus = {
    pending: number
}

// Sync now only enqueues work; actual delivery happens later on the retry worker's own tick, so
// there's nothing synchronous to await. This polls the queue depth instead to show real progress.
const SYNC_STATUS_POLL_INTERVAL_MS = 2000
// A poll reporting 0 pending is only trusted as "actually done" after this many consecutive
// zero polls in a row - otherwise the very first poll (which can land before seeding has even
// enqueued anything yet) would flip the indicator to "done" instantly.
const SYNC_STATUS_ZERO_GRACE_POLLS = 3
// Stop polling after this long regardless, so a stuck/failing sync doesn't poll forever.
const SYNC_STATUS_MAX_POLLS = 300

type PinResponse = {
    userCode: string
    verificationUri: string
    expiresIn: number
    interval: number
}

export function SimklSettingsContainer() {
    const queryClient = useQueryClient()
    const [pin, setPin] = React.useState<PinResponse | null>(null)
    const pollTimerRef = React.useRef<ReturnType<typeof setInterval> | null>(null)

    const { data: settings, refetch: refetchSettings } = useServerQuery<SimklSettings>({
        endpoint: "/api/v1/simkl/settings",
        method: "GET",
        queryKey: ["simkl-settings"],
    })

    const { mutate: startConnect, isPending: isStarting } = useServerMutation<PinResponse, void>({
        endpoint: "/api/v1/simkl/connect/start",
        method: "POST",
        mutationKey: ["simkl-connect-start"],
        onSuccess: (data) => {
            if (!data) return
            setPin(data)
            beginPolling(data)
        },
    })

    const { mutate: saveSettings } = useServerMutation<{ enabled?: boolean, clientId?: string }, { enabled?: boolean, clientId?: string }>({
        endpoint: "/api/v1/simkl/settings",
        method: "PATCH",
        mutationKey: ["simkl-save-settings"],
        onSuccess: () => {
            toast.success("SIMKL settings saved")
            refetchSettings()
        },
    })

    const [clientIdInput, setClientIdInput] = React.useState("")
    React.useEffect(() => {
        if (settings) setClientIdInput(settings.clientId)
    }, [settings?.clientId])
    const clientIdDirty = settings !== undefined && clientIdInput !== settings.clientId

    const { mutate: disconnect } = useServerMutation<boolean, void>({
        endpoint: "/api/v1/simkl/disconnect",
        method: "POST",
        mutationKey: ["simkl-disconnect"],
        onSuccess: () => {
            toast.success("SIMKL disconnected")
            refetchSettings()
        },
    })

    const [syncTracking, setSyncTracking] = React.useState(false)
    const [syncJustFinished, setSyncJustFinished] = React.useState(false)
    const zeroPollStreakRef = React.useRef(0)
    const pollCountRef = React.useRef(0)

    const { mutate: syncNow, isPending: isSyncing } = useServerMutation<boolean, void>({
        endpoint: "/api/v1/simkl/sync-now",
        method: "POST",
        mutationKey: ["simkl-sync-now"],
        onSuccess: () => {
            toast.success("Full SIMKL sync started")
            zeroPollStreakRef.current = 0
            pollCountRef.current = 0
            setSyncJustFinished(false)
            setSyncTracking(true)
        },
    })

    const { data: syncStatus, dataUpdatedAt: syncStatusUpdatedAt } = useServerQuery<SimklSyncStatus>({
        endpoint: "/api/v1/simkl/sync-status",
        method: "GET",
        queryKey: ["simkl-sync-status"],
        enabled: syncTracking,
        refetchInterval: syncTracking ? SYNC_STATUS_POLL_INTERVAL_MS : false,
        muteError: true,
    })

    React.useEffect(() => {
        if (!syncTracking || syncStatus === undefined) return

        // Keyed off dataUpdatedAt, not syncStatus itself: React Query's structural sharing
        // returns the SAME object reference across polls when the content is identical (e.g.
        // two consecutive {pending: 0} results), which would otherwise stop this effect from
        // re-running on every poll and permanently stall the zero-streak count below the
        // threshold - the exact "stuck at 0 items left" bug this fixes.
        pollCountRef.current += 1

        if (syncStatus.pending > 0) {
            zeroPollStreakRef.current = 0
        } else {
            zeroPollStreakRef.current += 1
        }

        const genuinelyDone = zeroPollStreakRef.current >= SYNC_STATUS_ZERO_GRACE_POLLS
        const gaveUp = pollCountRef.current >= SYNC_STATUS_MAX_POLLS

        if (genuinelyDone || gaveUp) {
            setSyncTracking(false)
            if (genuinelyDone) {
                setSyncJustFinished(true)
                toast.success("SIMKL sync complete")
            }
        }
    }, [syncStatusUpdatedAt, syncTracking])

    // The "complete" indicator is just a transient confirmation, not a persistent state - clear
    // it after a few seconds so the UI doesn't permanently claim a sync "just" finished.
    React.useEffect(() => {
        if (!syncJustFinished) return
        const id = setTimeout(() => setSyncJustFinished(false), 5000)
        return () => clearTimeout(id)
    }, [syncJustFinished])

    const { mutateAsync: pollConnect } = useServerMutation<boolean, { userCode: string }>({
        endpoint: "/api/v1/simkl/connect/poll",
        method: "POST",
        mutationKey: ["simkl-connect-poll"],
    })

    function beginPolling(activePin: PinResponse) {
        if (pollTimerRef.current) clearInterval(pollTimerRef.current)
        const startedAt = Date.now()
        pollTimerRef.current = setInterval(async () => {
            if (Date.now() - startedAt > activePin.expiresIn * 1000) {
                if (pollTimerRef.current) clearInterval(pollTimerRef.current)
                setPin(null)
                toast.error("SIMKL PIN expired, try again")
                return
            }
            try {
                const done = await pollConnect({ userCode: activePin.userCode })
                if (done === true) {
                    if (pollTimerRef.current) clearInterval(pollTimerRef.current)
                    setPin(null)
                    toast.success("SIMKL connected")
                    refetchSettings()
                    queryClient.invalidateQueries({ queryKey: ["simkl-settings"] })
                }
            } catch {
                // useServerMutation's built-in onError already shows a toast on genuine failures.
                // A "still pending" response is a 200 OK with data:false (not an error), so this
                // catch only fires on real errors — swallow here and let the next tick retry.
            }
        }, activePin.interval * 1000)
    }

    React.useEffect(() => {
        return () => {
            if (pollTimerRef.current) clearInterval(pollTimerRef.current)
        }
    }, [])

    const isConnected = settings?.connected ?? false
    const isEnabled = settings?.enabled ?? false

    const hasClientId = !!settings?.clientId

    return (
        <div className="space-y-4">
            <div className="flex items-center justify-between p-4 border rounded-[--radius-md]">
                <div>
                    <p className="font-medium">Enable SIMKL mirroring</p>
                    <p className="text-sm text-[--muted]">
                        Mirrors every AniList list change to SIMKL as a live backup. AniList remains the only
                        source used for browsing and playback.
                    </p>
                </div>
                <Switch
                    value={isEnabled}
                    onValueChange={(v) => saveSettings({ enabled: v })}
                />
            </div>

            {isEnabled && (
                <>
                    <div className="space-y-2 p-4 border rounded-[--radius-md]">
                        <p className="font-medium">SIMKL Client ID</p>
                        <p className="text-sm text-[--muted]">
                            Seanime needs your own SIMKL app to connect - register one (free, takes a minute) at{" "}
                            <SeaLink
                                href="https://simkl.com/settings/developer"
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-brand-400 hover:text-brand-300"
                            >
                                simkl.com/settings/developer
                            </SeaLink>. SIMKL requires a Redirect URI to create the app - enter{" "}
                            <code className="text-xs bg-gray-900 px-1 py-0.5 rounded">urn:ietf:wg:oauth:2.0:oob</code>{" "}
                            there (the standard value for PIN-based auth with no real redirect), then paste the
                            Client ID it gives you below.
                        </p>
                        <div className="flex items-center gap-2">
                            <input
                                type="text"
                                value={clientIdInput}
                                onChange={e => setClientIdInput(e.target.value)}
                                className="w-full max-w-md px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white text-sm focus:outline-none focus:border-brand-500"
                                placeholder="Paste your SIMKL Client ID..."
                            />
                            <Button
                                size="sm"
                                intent="gray-outline"
                                disabled={!clientIdDirty}
                                onClick={() => saveSettings({ clientId: clientIdInput.trim() })}
                            >
                                Save
                            </Button>
                        </div>
                    </div>

                    {!pin && !isConnected && (
                        <div className="flex items-center gap-3">
                            <p className="text-sm text-[--muted]">
                                {hasClientId ? "No SIMKL account connected" : "Set a Client ID above first"}
                            </p>
                            <Button
                                size="sm"
                                intent="gray-outline"
                                loading={isStarting}
                                disabled={!hasClientId}
                                onClick={() => startConnect()}
                            >
                                Connect SIMKL account
                            </Button>
                        </div>
                    )}

                    {pin && (
                        <Alert
                            intent="info"
                            description={`Go to ${pin.verificationUri} and enter code: ${pin.userCode}`}
                        />
                    )}

                    {isConnected && (
                        <div className="flex items-center gap-3 flex-wrap">
                            <p className="text-sm text-[--muted]">SIMKL account connected</p>
                            <Button
                                size="sm"
                                intent="gray-outline"
                                loading={isSyncing}
                                disabled={syncTracking}
                                onClick={() => syncNow()}
                            >
                                Sync now (seed full collection)
                            </Button>
                            <Button size="sm" intent="alert-subtle" onClick={() => disconnect()}>
                                Disconnect
                            </Button>
                            {syncTracking && (
                                <p className="text-sm text-[--muted] flex items-center gap-1.5">
                                    <LuLoaderCircle className="animate-spin" />
                                    Syncing{syncStatus ? ` (${syncStatus.pending} item${syncStatus.pending === 1 ? "" : "s"} left)` : "..."}
                                </p>
                            )}
                            {!syncTracking && syncJustFinished && (
                                <p className="text-sm text-green-400 flex items-center gap-1.5">
                                    <LuCircleCheck />
                                    Sync complete
                                </p>
                            )}
                        </div>
                    )}
                </>
            )}
        </div>
    )
}
