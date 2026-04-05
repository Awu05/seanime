import { useAuthLogout } from "@/api/hooks/auth.hooks"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { useAtom } from "jotai"
import React from "react"
import { createPortal } from "react-dom"

export function ProfileIndicator() {
    const [profile, setProfile] = useAtom(currentProfileAtom)
    const { mutate: logout } = useAuthLogout()
    const [open, setOpen] = React.useState(false)
    const buttonRef = React.useRef<HTMLButtonElement>(null)
    const [menuPos, setMenuPos] = React.useState({ top: 0, left: 0 })

    if (!profile) return null

    function handleOpen() {
        if (buttonRef.current) {
            const rect = buttonRef.current.getBoundingClientRect()
            setMenuPos({
                top: rect.top - 8,
                left: rect.left,
            })
        }
        setOpen(!open)
    }

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
        <>
            <button
                ref={buttonRef}
                onClick={handleOpen}
                className="flex items-center gap-2 px-2 py-1 rounded-lg hover:bg-gray-800 transition-colors"
            >
                <div className="w-7 h-7 rounded-full bg-gradient-to-br from-brand-500 to-brand-700 flex items-center justify-center text-white text-xs font-bold">
                    {profile.name?.[0]?.toUpperCase()}
                </div>
                <span className="text-sm text-gray-300 hidden md:inline">{profile.name}</span>
            </button>

            {open && createPortal(
                <>
                    <div className="fixed inset-0 z-[9998]" onClick={() => setOpen(false)} />
                    <div
                        className="fixed w-48 bg-gray-900 border border-gray-700 rounded-lg shadow-xl z-[9999] py-1"
                        style={{
                            top: menuPos.top,
                            left: menuPos.left,
                            transform: "translateY(-100%)",
                        }}
                    >
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
                </>,
                document.body,
            )}
        </>
    )
}
