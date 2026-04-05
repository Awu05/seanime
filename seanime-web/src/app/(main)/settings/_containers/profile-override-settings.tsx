import { useGetProfileSettings, useSaveProfileSettings } from "@/api/hooks/auth.hooks"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { SettingsCard } from "@/app/(main)/settings/_components/settings-card"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { useQueryClient } from "@tanstack/react-query"
import { useAtomValue } from "jotai"
import React from "react"
import { toast } from "sonner"

type OverridableSettings = {
    torrentProvider?: string
    hideAudienceScore?: boolean
    enableAdultContent?: boolean
    blurAdultContent?: boolean
    autoUpdateProgress?: boolean
    autoPlayNextEpisode?: boolean
    enableWatchContinuity?: boolean
    disableAnimeCardTrailers?: boolean
    enableOnlinestream?: boolean
    enableManga?: boolean
}

const BOOL_FIELDS: { key: keyof OverridableSettings; label: string }[] = [
    { key: "autoUpdateProgress", label: "Auto-update progress" },
    { key: "autoPlayNextEpisode", label: "Auto-play next episode" },
    { key: "enableWatchContinuity", label: "Watch continuity" },
    { key: "hideAudienceScore", label: "Hide audience score" },
    { key: "enableAdultContent", label: "Enable adult content" },
    { key: "blurAdultContent", label: "Blur adult content" },
    { key: "disableAnimeCardTrailers", label: "Disable anime card trailers" },
    { key: "enableOnlinestream", label: "Enable online streaming" },
    { key: "enableManga", label: "Enable manga" },
]

export function ProfileOverrideSettings() {
    const profile = useAtomValue(currentProfileAtom)
    const qc = useQueryClient()
    const { data } = useGetProfileSettings()
    const { mutate: save, isPending } = useSaveProfileSettings()

    const [overrides, setOverrides] = React.useState<OverridableSettings>({})

    React.useEffect(() => {
        if (data?.overrides) {
            try {
                setOverrides(JSON.parse(data.overrides))
            } catch {
                setOverrides({})
            }
        }
    }, [data?.overrides])

    if (!profile) return null

    function handleToggleField(key: keyof OverridableSettings) {
        setOverrides(prev => {
            const next = { ...prev }
            if (key in next) {
                delete next[key]
            } else {
                next[key] = false
            }
            return next
        })
    }

    function handleChangeValue(key: keyof OverridableSettings, value: boolean) {
        setOverrides(prev => ({ ...prev, [key]: value }))
    }

    function handleSave() {
        save({ overrides: JSON.stringify(overrides) }, {
            onSuccess: () => {
                qc.invalidateQueries({ queryKey: [API_ENDPOINTS.AUTH.GetProfileSettings.key] })
                toast.success("Profile settings saved")
            },
            onError: () => toast.error("Failed to save settings"),
        })
    }

    return (
        <div className="space-y-4">
            <SettingsCard
                title="Personal Overrides"
                description="Override global settings for your profile. Unset fields use the admin's defaults."
            >
                <div className="space-y-3">
                    {BOOL_FIELDS.map(({ key, label }) => {
                        const isOverridden = key in overrides
                        return (
                            <div key={key} className="flex items-center justify-between py-2 border-b border-gray-800 last:border-0">
                                <div className="flex-1">
                                    <p className="text-sm font-medium text-white">{label}</p>
                                    <p className="text-xs text-gray-500">
                                        {isOverridden ? "Custom value" : "Using default"}
                                    </p>
                                </div>
                                <div className="flex items-center gap-3">
                                    {isOverridden && (
                                        <Switch
                                            value={overrides[key] as boolean}
                                            onValueChange={v => handleChangeValue(key, v)}
                                        />
                                    )}
                                    <button
                                        onClick={() => handleToggleField(key)}
                                        className={`text-xs px-2 py-1 rounded ${
                                            isOverridden
                                                ? "bg-brand-500/20 text-brand-400"
                                                : "bg-gray-800 text-gray-400"
                                        }`}
                                    >
                                        {isOverridden ? "Custom" : "Default"}
                                    </button>
                                </div>
                            </div>
                        )
                    })}
                </div>

                <Button
                    onClick={handleSave}
                    loading={isPending}
                    className="mt-4 w-full"
                >
                    Save Overrides
                </Button>
            </SettingsCard>
        </div>
    )
}
