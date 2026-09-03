import { useServerMutation, useServerQuery } from "@/api/client/requests"
import { Alert } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { useQueryClient } from "@tanstack/react-query"
import React from "react"
import { toast } from "sonner"

type SimklSettings = {
    enabled: boolean
    connected: boolean
}

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

    const { mutate: saveSettings } = useServerMutation<{ enabled: boolean }, { enabled: boolean }>({
        endpoint: "/api/v1/simkl/settings",
        method: "PATCH",
        mutationKey: ["simkl-save-settings"],
        onSuccess: () => {
            toast.success("SIMKL settings saved")
            refetchSettings()
        },
    })

    const { mutate: disconnect } = useServerMutation<boolean, void>({
        endpoint: "/api/v1/simkl/disconnect",
        method: "POST",
        mutationKey: ["simkl-disconnect"],
        onSuccess: () => {
            toast.success("SIMKL disconnected")
            refetchSettings()
        },
    })

    const { mutate: syncNow, isPending: isSyncing } = useServerMutation<boolean, void>({
        endpoint: "/api/v1/simkl/sync-now",
        method: "POST",
        mutationKey: ["simkl-sync-now"],
        onSuccess: () => {
            toast.success("Full SIMKL sync started")
        },
    })

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
                    value={settings?.enabled ?? false}
                    onValueChange={(v) => saveSettings({ enabled: v })}
                />
            </div>

            {!pin && (
                <div className="flex items-center gap-3">
                    <p className="text-sm text-[--muted]">
                        {isConnected ? "SIMKL account connected" : "No SIMKL account connected"}
                    </p>
                    <Button size="sm" intent="gray-outline" loading={isStarting} onClick={() => startConnect()}>
                        {isConnected ? "Reconnect" : "Connect SIMKL account"}
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
                <div className="flex gap-2">
                    <Button size="sm" intent="gray-outline" loading={isSyncing} onClick={() => syncNow()}>
                        Sync now (seed full collection)
                    </Button>
                    <Button size="sm" intent="alert-subtle" onClick={() => disconnect()}>
                        Disconnect
                    </Button>
                </div>
            )}
        </div>
    )
}
