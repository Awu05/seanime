import { useAuthGetProfiles, useAuthSelectProfile } from "@/api/hooks/auth.hooks"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { buildSeaQuery } from "@/api/client/requests"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { useQueryClient } from "@tanstack/react-query"
import { useSetAtom } from "jotai"
import React from "react"
import { toast } from "sonner"

export function ProfilePickerPage() {
    const qc = useQueryClient()
    const setProfile = useSetAtom(currentProfileAtom)
    const { data: profiles } = useAuthGetProfiles()
    const { mutate: selectProfile, isPending } = useAuthSelectProfile()
    const [pinFor, setPinFor] = React.useState<string | null>(null)
    const [pin, setPin] = React.useState("")
    const [error, setError] = React.useState("")
    const [showCreate, setShowCreate] = React.useState(false)
    const [newName, setNewName] = React.useState("")
    const [isCreating, setIsCreating] = React.useState(false)

    function storeProfileAndRedirect(data: any) {
        if (data?.profile) {
            setProfile({
                id: data.profile.id,
                name: data.profile.name,
                isAdmin: data.profile.isAdmin,
                avatar: data.profile.avatar,
            })
        }
        window.location.href = "/"
    }

    function handleSelect(profileId: string, hasPin: boolean) {
        if (hasPin) {
            setPinFor(profileId)
            setPin("")
            setError("")
            return
        }
        selectProfile({ profileId }, {
            onSuccess: (data) => storeProfileAndRedirect(data),
            onError: () => setError("Failed to select profile"),
        })
    }

    function handlePinSubmit(e: React.FormEvent) {
        e.preventDefault()
        if (!pinFor) return
        selectProfile({ profileId: pinFor, pin }, {
            onSuccess: (data) => storeProfileAndRedirect(data),
            onError: () => setError("Invalid PIN"),
        })
    }

    function handleCreateProfile(e: React.FormEvent) {
        e.preventDefault()
        if (!newName.trim() || isCreating) return
        setIsCreating(true)
        buildSeaQuery({
            endpoint: "/api/v1/auth/create-profile",
            method: "POST",
            data: { name: newName.trim() },
        })
            .then(() => {
                setNewName("")
                setShowCreate(false)
                qc.invalidateQueries({ queryKey: [API_ENDPOINTS.USER_AUTH.GetProfiles.key] })
                toast.success("Profile created")
            })
            .catch(() => {
                toast.error("Failed to create profile")
            })
            .finally(() => setIsCreating(false))
    }

    if (pinFor) {
        return (
            <div className="space-y-6">
                <div className="text-center">
                    <h1 className="text-2xl font-bold text-white">Enter PIN</h1>
                </div>
                <form onSubmit={handlePinSubmit} className="space-y-4">
                    <input
                        type="password"
                        value={pin}
                        onChange={e => setPin(e.target.value)}
                        placeholder="PIN"
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white text-center text-2xl tracking-widest focus:outline-none focus:border-brand-500"
                        maxLength={6}
                        autoFocus
                    />
                    {error && <p className="text-red-400 text-sm text-center">{error}</p>}
                    <div className="flex gap-2">
                        <button
                            type="button"
                            onClick={() => setPinFor(null)}
                            className="flex-1 py-2 bg-gray-800 text-white rounded-lg"
                        >
                            Back
                        </button>
                        <button
                            type="submit"
                            disabled={isPending}
                            className="flex-1 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg disabled:opacity-50"
                        >
                            Confirm
                        </button>
                    </div>
                </form>
            </div>
        )
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Who's watching?</h1>
            </div>
            <div className="flex flex-wrap justify-center gap-4">
                {profiles?.map((profile: any) => (
                    <button
                        key={profile.id}
                        onClick={() => handleSelect(profile.id, !!profile.hasPin)}
                        className="flex flex-col items-center gap-2 p-4 rounded-lg border border-gray-700 hover:border-brand-500 transition-all"
                    >
                        <div className="w-16 h-16 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-xl font-bold">
                            {profile.name?.[0]?.toUpperCase()}
                        </div>
                        <span className="text-white font-medium">{profile.name}</span>
                    </button>
                ))}

                {/* Add Profile button */}
                {!showCreate && (
                    <button
                        onClick={() => setShowCreate(true)}
                        className="flex flex-col items-center gap-2 p-4 rounded-lg border border-dashed border-gray-700 hover:border-gray-500 transition-all"
                    >
                        <div className="w-16 h-16 rounded-full bg-gray-800 flex items-center justify-center text-gray-400 text-3xl">
                            +
                        </div>
                        <span className="text-gray-400 font-medium">Add Profile</span>
                    </button>
                )}
            </div>

            {/* Inline profile creation form */}
            {showCreate && (
                <form onSubmit={handleCreateProfile} className="space-y-3">
                    <input
                        type="text"
                        value={newName}
                        onChange={e => setNewName(e.target.value)}
                        placeholder="Profile name"
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                        autoFocus
                        required
                    />
                    <div className="flex gap-2">
                        <button
                            type="button"
                            onClick={() => { setShowCreate(false); setNewName("") }}
                            className="flex-1 py-2 bg-gray-800 text-white rounded-lg"
                        >
                            Cancel
                        </button>
                        <button
                            type="submit"
                            disabled={isCreating || !newName.trim()}
                            className="flex-1 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg disabled:opacity-50"
                        >
                            {isCreating ? "Creating..." : "Create"}
                        </button>
                    </div>
                </form>
            )}
        </div>
    )
}
