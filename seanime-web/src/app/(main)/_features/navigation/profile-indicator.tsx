import { useAuthLogout } from "@/api/hooks/auth.hooks"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { useAtom } from "jotai"
import React from "react"

export function ProfileIndicator() {
    const [profile, setProfile] = useAtom(currentProfileAtom)
    const { mutate: logout } = useAuthLogout()
    const [open, setOpen] = React.useState(false)

    if (!profile) return null

    function handleSwitchProfile() {
        setProfile(null)
        window.location.href = "/profiles"
    }

    function handleLogout() {
        logout(undefined, {
            onSuccess: () => {
                setProfile(null)
                window.location.href = "/login"
            },
        })
    }

    return (
        <div className="relative">
            <button
                onClick={() => setOpen(!open)}
                className="flex items-center gap-2 px-2 py-1 rounded-lg hover:bg-gray-800 transition-colors"
            >
                <div className="w-7 h-7 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-xs font-bold">
                    {profile.name?.[0]?.toUpperCase()}
                </div>
                <span className="text-sm text-gray-300 hidden md:inline">{profile.name}</span>
            </button>

            {open && (
                <>
                    <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
                    <div className="absolute bottom-full left-0 mb-2 w-48 bg-gray-900 border border-gray-700 rounded-lg shadow-xl z-50 py-1">
                        <button
                            onClick={handleSwitchProfile}
                            className="w-full text-left px-4 py-2 text-sm text-gray-300 hover:bg-gray-800"
                        >
                            Switch Profile
                        </button>
                        <button
                            onClick={handleLogout}
                            className="w-full text-left px-4 py-2 text-sm text-red-400 hover:bg-gray-800"
                        >
                            Logout
                        </button>
                    </div>
                </>
            )}
        </div>
    )
}
