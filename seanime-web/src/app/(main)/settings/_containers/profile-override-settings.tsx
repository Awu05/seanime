import { useGetProfileSettings, useSaveProfileSettings } from "@/api/hooks/auth.hooks"
import { buildSeaQuery } from "@/api/client/requests"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { SettingsCard } from "@/app/(main)/settings/_components/settings-card"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { useQueryClient } from "@tanstack/react-query"
import { useAtom } from "jotai"
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
    const [profile, setProfile] = useAtom(currentProfileAtom)
    const qc = useQueryClient()
    const { data } = useGetProfileSettings()
    const { mutate: save, isPending } = useSaveProfileSettings()

    const [overrides, setOverrides] = React.useState<OverridableSettings>({})
    const [profileName, setProfileName] = React.useState("")
    const [isSavingName, setIsSavingName] = React.useState(false)
    const [newPin, setNewPin] = React.useState("")
    const [isSavingPin, setIsSavingPin] = React.useState(false)

    React.useEffect(() => {
        if (profile?.name) setProfileName(profile.name)
    }, [profile?.name])

    function handleSaveName(e: React.FormEvent) {
        e.preventDefault()
        if (!profile || !profileName.trim() || isSavingName) return
        setIsSavingName(true)
        buildSeaQuery({
            endpoint: `/api/v1/profiles/${profile.id}/name`,
            method: "POST",
            data: { name: profileName.trim() },
        })
            .then(() => {
                setProfile({ ...profile, name: profileName.trim() })
                toast.success("Profile name updated")
            })
            .catch(() => toast.error("Failed to update name"))
            .finally(() => setIsSavingName(false))
    }

    function handleSavePin(e: React.FormEvent) {
        e.preventDefault()
        if (!profile || isSavingPin) return
        setIsSavingPin(true)
        buildSeaQuery({
            endpoint: `/api/v1/profiles/${profile.id}/pin`,
            method: "POST",
            data: { pin: newPin },
        })
            .then(() => {
                setNewPin("")
                toast.success(newPin ? "PIN set" : "PIN removed")
            })
            .catch(() => toast.error("Failed to update PIN"))
            .finally(() => setIsSavingPin(false))
    }

    React.useEffect(() => {
        if (data?.overrides) {
            try {
                setOverrides(JSON.parse(data.overrides) as OverridableSettings)
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
                (next as any)[key] = false
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
            <SettingsCard title="Profile">
                <form onSubmit={handleSaveName} className="flex gap-2 items-end">
                    <div className="flex-1">
                        <label className="block text-sm text-gray-300 mb-1">Display Name</label>
                        <input
                            type="text"
                            value={profileName}
                            onChange={e => setProfileName(e.target.value)}
                            className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                            required
                        />
                    </div>
                    <Button
                        type="submit"
                        loading={isSavingName}
                        disabled={!profileName.trim() || profileName.trim() === profile?.name}
                        intent="primary-subtle"
                    >
                        Save
                    </Button>
                </form>

                <div className="border-t border-gray-800 mt-4 pt-4">
                    <form onSubmit={handleSavePin} className="flex gap-2 items-end">
                        <div className="flex-1">
                            <label className="block text-sm text-gray-300 mb-1">Profile PIN</label>
                            <input
                                type="password"
                                value={newPin}
                                onChange={e => setNewPin(e.target.value)}
                                placeholder="Enter new PIN (4-6 digits)"
                                maxLength={6}
                                className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                            />
                        </div>
                        <Button
                            type="submit"
                            loading={isSavingPin}
                            intent="primary-subtle"
                        >
                            {newPin ? "Set PIN" : "Remove PIN"}
                        </Button>
                    </form>
                    <p className="text-xs text-gray-500 mt-1">Leave empty and click "Remove PIN" to remove the PIN from your profile.</p>
                </div>
            </SettingsCard>

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
