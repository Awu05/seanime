import { useAuthGetProfiles, useAuthSelectProfile } from "@/api/hooks/auth.hooks"
import { useNavigate } from "@tanstack/react-router"
import React from "react"

export function ProfilePickerPage() {
    const navigate = useNavigate()
    const { data: profiles } = useAuthGetProfiles()
    const { mutate: selectProfile, isPending } = useAuthSelectProfile()
    const [pinFor, setPinFor] = React.useState<string | null>(null)
    const [pin, setPin] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSelect(profileId: string, hasPin: boolean) {
        if (hasPin) {
            setPinFor(profileId)
            setPin("")
            setError("")
            return
        }
        selectProfile({ profileId }, {
            onSuccess: () => navigate({ to: "/" }),
            onError: () => setError("Failed to select profile"),
        })
    }

    function handlePinSubmit(e: React.FormEvent) {
        e.preventDefault()
        if (!pinFor) return
        selectProfile({ profileId: pinFor, pin }, {
            onSuccess: () => navigate({ to: "/" }),
            onError: () => setError("Invalid PIN"),
        })
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
            <div className="grid grid-cols-2 gap-4">
                {profiles?.map((profile: any) => (
                    <button
                        key={profile.id}
                        onClick={() => handleSelect(profile.id, !!profile.pinHash)}
                        className="flex flex-col items-center gap-2 p-4 rounded-lg border border-gray-700 hover:border-brand-500 transition-all"
                    >
                        <div className="w-16 h-16 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-xl font-bold">
                            {profile.name?.[0]?.toUpperCase()}
                        </div>
                        <span className="text-white font-medium">{profile.name}</span>
                    </button>
                ))}
            </div>
        </div>
    )
}
