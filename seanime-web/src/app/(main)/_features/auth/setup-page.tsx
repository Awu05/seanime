import { useAuthSetup } from "@/api/hooks/auth.hooks"
import React from "react"

export function SetupPage() {
    const { mutate: setup, isPending } = useAuthSetup()
    const [username, setUsername] = React.useState("")
    const [password, setPassword] = React.useState("")
    const [confirmPassword, setConfirmPassword] = React.useState("")
    const [enableAccessCode, setEnableAccessCode] = React.useState(false)
    const [accessCode, setAccessCode] = React.useState("")
    const [error, setError] = React.useState("")

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")

        if (!username.trim() || !password.trim()) {
            setError("Username and password are required")
            return
        }

        if (password !== confirmPassword) {
            setError("Passwords do not match")
            return
        }

        setup({ username: username.trim(), password, confirmPassword, accessCode: enableAccessCode ? accessCode.trim() : undefined }, {
            onSuccess: () => {
                window.location.href = "/login"
            },
            onError: (err: any) => {
                setError(err?.response?.data?.error || "Failed to create admin account")
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
                <div>
                    <label className="block text-sm text-gray-300 mb-1">Confirm Password</label>
                    <input
                        type="password"
                        value={confirmPassword}
                        onChange={e => setConfirmPassword(e.target.value)}
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                        required
                    />
                </div>
                <div className="space-y-2">
                    <label className="flex items-center gap-2 cursor-pointer">
                        <input
                            type="checkbox"
                            checked={enableAccessCode}
                            onChange={e => {
                                setEnableAccessCode(e.target.checked)
                                if (!e.target.checked) setAccessCode("")
                            }}
                            className="w-4 h-4 rounded border-gray-700 bg-gray-900 text-brand-500 focus:ring-brand-500"
                        />
                        <span className="text-sm text-gray-300">Set an access code</span>
                    </label>
                    <p className="text-xs text-gray-500 ml-6">
                        An access code lets other household members access Seanime and create their own profiles
                        without needing the admin password.
                    </p>
                    {enableAccessCode && (
                        <input
                            type="text"
                            value={accessCode}
                            onChange={e => setAccessCode(e.target.value)}
                            placeholder="Enter access code"
                            className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                        />
                    )}
                </div>
                {error && <p className="text-red-400 text-sm">{error}</p>}
                <button
                    type="submit"
                    disabled={isPending}
                    className="w-full py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                >
                    {isPending ? "Setting up..." : "Create Account"}
                </button>
            </form>
        </div>
    )
}
