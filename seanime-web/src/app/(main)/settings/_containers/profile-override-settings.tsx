import { buildSeaQuery } from "@/api/client/requests"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { SettingsCard } from "@/app/(main)/settings/_components/settings-card"
import { Button } from "@/components/ui/button"
import { useAtom } from "jotai"
import React from "react"
import { toast } from "sonner"

export function ProfileOverrideSettings() {
    const [profile, setProfile] = useAtom(currentProfileAtom)

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
        </div>
    )
}
