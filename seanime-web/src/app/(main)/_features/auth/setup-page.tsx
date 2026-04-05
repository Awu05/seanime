import { useAuthSetup } from "@/api/hooks/auth.hooks"
import { useNavigate } from "@tanstack/react-router"
import React from "react"

export function SetupPage() {
    const navigate = useNavigate()
    const { mutate: setup, isPending } = useAuthSetup()
    const [username, setUsername] = React.useState("")
    const [password, setPassword] = React.useState("")
    const [accessCode, setAccessCode] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")
        setup({ username, password, accessCode: accessCode || undefined }, {
            onSuccess: () => {
                navigate({ to: "/login" })
            },
            onError: () => {
                setError("Failed to create admin account")
            },
        })
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Welcome to Seanime</h1>
                <p className="text-gray-400 mt-2">Create your admin account to get started</p>
            </div>
            <form onSubmit={handleSubmit} className="space-y-4">
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Admin Username</label>
                    <input
                        type="text"
                        value={username}
                        onChange={e => setUsername(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                        required
                    />
                </div>
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Admin Password</label>
                    <input
                        type="password"
                        value={password}
                        onChange={e => setPassword(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                        required
                    />
                </div>
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Household Access Code (optional)</label>
                    <p className="text-xs text-gray-500 mb-1">Other household members use this to access the profile picker</p>
                    <input
                        type="text"
                        value={accessCode}
                        onChange={e => setAccessCode(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                    />
                </div>
                {error && <p className="text-red-400 text-sm">{error}</p>}
                <button
                    type="submit"
                    disabled={isPending}
                    className="w-full py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                >
                    {isPending ? "Setting up..." : "Create Admin Account"}
                </button>
            </form>
        </div>
    )
}
