import { useAuthGetProfiles } from "@/api/hooks/auth.hooks"
import { useCreateProfile, useDeleteProfile, useSetInstanceAccessCode } from "@/api/hooks/profile-management.hooks"
import { useQueryClient } from "@tanstack/react-query"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import React from "react"
import { toast } from "sonner"

export function ProfileManagementSettings() {
    const qc = useQueryClient()
    const { data: profiles } = useAuthGetProfiles()
    const { mutate: createProfile, isPending: isCreating } = useCreateProfile()
    const { mutate: deleteProfile } = useDeleteProfile()
    const { mutate: setAccessCode } = useSetInstanceAccessCode()

    const [newName, setNewName] = React.useState("")
    const [newAccessCode, setNewAccessCode] = React.useState("")

    function handleCreateProfile(e: React.FormEvent) {
        e.preventDefault()
        if (!newName.trim()) return
        createProfile({ name: newName.trim() }, {
            onSuccess: () => {
                setNewName("")
                qc.invalidateQueries({ queryKey: [API_ENDPOINTS.AUTH.GetProfiles.key] })
                toast.success("Profile created")
            },
            onError: () => toast.error("Failed to create profile"),
        })
    }

    function handleDeleteProfile(id: string, name: string) {
        if (!confirm(`Delete profile "${name}"?`)) return
        deleteProfile({ id }, {
            onSuccess: () => {
                qc.invalidateQueries({ queryKey: [API_ENDPOINTS.AUTH.GetProfiles.key] })
                toast.success("Profile deleted")
            },
            onError: () => toast.error("Failed to delete profile"),
        })
    }

    function handleSetAccessCode(e: React.FormEvent) {
        e.preventDefault()
        setAccessCode({ accessCode: newAccessCode }, {
            onSuccess: () => {
                setNewAccessCode("")
                toast.success("Access code updated")
            },
            onError: () => toast.error("Failed to update access code"),
        })
    }

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-lg font-semibold text-white mb-4">Profiles</h3>
                <div className="space-y-2">
                    {profiles?.map((profile: any) => (
                        <div key={profile.id} className="flex items-center justify-between p-3 bg-gray-900 rounded-lg border border-gray-800">
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white font-bold">
                                    {profile.name?.[0]?.toUpperCase()}
                                </div>
                                <div>
                                    <p className="font-medium text-white">{profile.name}</p>
                                    {profile.isAdmin && <span className="text-xs text-brand-400">Admin</span>}
                                </div>
                            </div>
                            {!profile.isAdmin && (
                                <button
                                    onClick={() => handleDeleteProfile(profile.id, profile.name)}
                                    className="text-sm text-red-400 hover:text-red-300"
                                >
                                    Delete
                                </button>
                            )}
                        </div>
                    ))}
                </div>

                <form onSubmit={handleCreateProfile} className="flex gap-2 mt-4">
                    <input
                        type="text"
                        value={newName}
                        onChange={e => setNewName(e.target.value)}
                        placeholder="New profile name"
                        className="flex-1 px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                    />
                    <button
                        type="submit"
                        disabled={isCreating || !newName.trim()}
                        className="px-4 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg disabled:opacity-50"
                    >
                        Add
                    </button>
                </form>
            </div>

            <div>
                <h3 className="text-lg font-semibold text-white mb-4">Instance Access Code</h3>
                <p className="text-sm text-gray-400 mb-2">Household members enter this code to access the profile picker.</p>
                <form onSubmit={handleSetAccessCode} className="flex gap-2">
                    <input
                        type="text"
                        value={newAccessCode}
                        onChange={e => setNewAccessCode(e.target.value)}
                        placeholder="New access code"
                        className="flex-1 px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white"
                    />
                    <button
                        type="submit"
                        className="px-4 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg"
                    >
                        Update
                    </button>
                </form>
            </div>
        </div>
    )
}
