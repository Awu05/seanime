import { useAuthAdminLogin } from "@/api/hooks/auth.hooks"
import { currentProfileAtom } from "@/app/(main)/_atoms/profile.atoms"
import { useSetAtom } from "jotai"
import React from "react"

export function LoginPage() {
    const { mutate: login, isPending } = useAuthAdminLogin()
    const setProfile = useSetAtom(currentProfileAtom)
    const [username, setUsername] = React.useState("")
    const [password, setPassword] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")
        login({ username, password }, {
            onSuccess: (data) => {
                if (data?.profile) {
                    setProfile({
                        id: data.profile.id,
                        name: data.profile.name,
                        isAdmin: data.profile.isAdmin,
                        avatar: data.profile.avatar,
                    })
                }
                window.location.href = "/"
            },
            onError: () => {
                setError("Invalid credentials")
            },
        })
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Login</h1>
                <p className="text-gray-400 mt-2">Sign in to your seanime instance</p>
            </div>
            <form onSubmit={handleSubmit} className="space-y-4">
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Username</label>
                    <input
                        type="text"
                        value={username}
                        onChange={e => setUsername(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                        required
                    />
                </div>
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Password</label>
                    <input
                        type="password"
                        value={password}
                        onChange={e => setPassword(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                        required
                    />
                </div>
                {error && <p className="text-red-400 text-sm">{error}</p>}
                <button
                    type="submit"
                    disabled={isPending}
                    className="w-full py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                >
                    {isPending ? "Signing in..." : "Sign In"}
                </button>
            </form>
            <div className="text-center">
                <button
                    onClick={() => window.location.href = "/access"}
                    className="text-sm text-gray-400 hover:text-white"
                >
                    Enter with access code instead
                </button>
            </div>
        </div>
    )
}
