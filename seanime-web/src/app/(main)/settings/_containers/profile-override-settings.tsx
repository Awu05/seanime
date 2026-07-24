import { buildSeaQuery } from "@/api/client/requests"
import { useAnimeListTorrentProviderExtensions } from "@/api/hooks/extensions.hooks"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { SettingsCard } from "@/app/(main)/settings/_components/settings-card"
import { Button } from "@/components/ui/button"
import { useAtom } from "jotai"
import React from "react"
import { toast } from "sonner"
import { useGetProfileSettings, useSaveProfileSettings } from "@/api/hooks/auth.hooks"

// Mirrors internal/core/profile_context.go OverridableSettings. All fields
// optional/omitempty: absent means "inherit the global setting".
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

const INHERIT = "__inherit__"

// Tri-state control for a boolean override: inherit the global value, or force on/off.
function OverrideBoolField({ label, value, onChange }: {
    label: string
    value: boolean | undefined
    onChange: (value: boolean | undefined) => void
}) {
    const selected = value === undefined ? INHERIT : value ? "on" : "off"
    return (
        <div className="flex items-center justify-between gap-4 py-1.5">
            <label className="text-sm text-gray-300">{label}</label>
            <select
                value={selected}
                onChange={e => {
                    const v = e.target.value
                    onChange(v === INHERIT ? undefined : v === "on")
                }}
                className="px-2 py-1 bg-gray-900 border border-gray-700 rounded text-white text-sm focus:outline-none focus:border-brand-500"
            >
                <option value={INHERIT}>Inherit</option>
                <option value="on">On</option>
                <option value="off">Off</option>
            </select>
        </div>
    )
}

export function ProfileOverrideSettings() {
    const [profile, setProfile] = useAtom(currentProfileAtom)

    const [profileName, setProfileName] = React.useState("")
    const [isSavingName, setIsSavingName] = React.useState(false)
    const [newPin, setNewPin] = React.useState("")
    const [isSavingPin, setIsSavingPin] = React.useState(false)

    const { data: profileSettings, isLoading: isLoadingOverrides } = useGetProfileSettings()
    const { mutate: saveOverrides, isPending: isSavingOverrides } = useSaveProfileSettings()
    const { data: torrentProviderExtensions } = useAnimeListTorrentProviderExtensions()

    const [overrides, setOverrides] = React.useState<OverridableSettings>({})
    const [overridesDirty, setOverridesDirty] = React.useState(false)

    React.useEffect(() => {
        if (profileSettings?.overrides && !overridesDirty) {
            try {
                setOverrides(JSON.parse(profileSettings.overrides) || {})
            }
            catch {
                setOverrides({})
            }
        }
    }, [profileSettings?.overrides])

    function updateOverride<K extends keyof OverridableSettings>(key: K, value: OverridableSettings[K]) {
        setOverrides(prev => ({ ...prev, [key]: value }))
        setOverridesDirty(true)
    }

    function handleSaveOverrides() {
        saveOverrides({ overrides: JSON.stringify(overrides) }, {
            onSuccess: () => {
                toast.success("Profile overrides saved")
                setOverridesDirty(false)
            },
            onError: () => toast.error("Failed to save profile overrides"),
        })
    }

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

    if (!profile) return null

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

            <SettingsCard title="My preferences" description="Override the household's global settings for your profile only. Anything left on Inherit follows the global setting.">
                {!isLoadingOverrides && <div className="space-y-1 divide-y divide-gray-800">
                    <div className="flex items-center justify-between gap-4 py-1.5">
                        <label className="text-sm text-gray-300">Preferred torrent provider</label>
                        <select
                            value={overrides.torrentProvider || INHERIT}
                            onChange={e => updateOverride("torrentProvider", e.target.value === INHERIT ? undefined : e.target.value)}
                            className="px-2 py-1 bg-gray-900 border border-gray-700 rounded text-white text-sm focus:outline-none focus:border-brand-500"
                        >
                            <option value={INHERIT}>Inherit</option>
                            {torrentProviderExtensions?.map(ext => (
                                <option key={ext.id} value={ext.id}>{ext.name}</option>
                            ))}
                        </select>
                    </div>
                    <OverrideBoolField
                        label="Hide audience score"
                        value={overrides.hideAudienceScore}
                        onChange={v => updateOverride("hideAudienceScore", v)}
                    />
                    <OverrideBoolField
                        label="Enable adult content"
                        value={overrides.enableAdultContent}
                        onChange={v => updateOverride("enableAdultContent", v)}
                    />
                    <OverrideBoolField
                        label="Blur adult content"
                        value={overrides.blurAdultContent}
                        onChange={v => updateOverride("blurAdultContent", v)}
                    />
                    <OverrideBoolField
                        label="Auto-update AniList progress"
                        value={overrides.autoUpdateProgress}
                        onChange={v => updateOverride("autoUpdateProgress", v)}
                    />
                    <OverrideBoolField
                        label="Auto-play next episode"
                        value={overrides.autoPlayNextEpisode}
                        onChange={v => updateOverride("autoPlayNextEpisode", v)}
                    />
                    <OverrideBoolField
                        label="Watch continuity"
                        value={overrides.enableWatchContinuity}
                        onChange={v => updateOverride("enableWatchContinuity", v)}
                    />
                    <OverrideBoolField
                        label="Anime card trailers"
                        value={overrides.disableAnimeCardTrailers === undefined ? undefined : !overrides.disableAnimeCardTrailers}
                        onChange={v => updateOverride("disableAnimeCardTrailers", v === undefined ? undefined : !v)}
                    />
                    <OverrideBoolField
                        label="Online streaming"
                        value={overrides.enableOnlinestream}
                        onChange={v => updateOverride("enableOnlinestream", v)}
                    />
                    <OverrideBoolField
                        label="Manga"
                        value={overrides.enableManga}
                        onChange={v => updateOverride("enableManga", v)}
                    />
                </div>}
                <div className="flex justify-end mt-4">
                    <Button
                        intent="primary-subtle"
                        loading={isSavingOverrides}
                        disabled={!overridesDirty}
                        onClick={handleSaveOverrides}
                    >
                        Save preferences
                    </Button>
                </div>
            </SettingsCard>
        </div>
    )
}
