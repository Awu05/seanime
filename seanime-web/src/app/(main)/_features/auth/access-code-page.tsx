import { useAuthAccessCode } from "@/api/hooks/auth.hooks"
import React from "react"

export function AccessCodePage() {
    const { mutate: submitCode, isPending } = useAuthAccessCode()
    const [accessCode, setAccessCode] = React.useState("")
    const [rememberMe, setRememberMe] = React.useState(false)
    const [error, setError] = React.useState("")

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")
        submitCode({ accessCode, rememberMe }, {
            onSuccess: () => {
                window.location.href = "/profiles"
            },
            onError: () => {
                setError("Invalid access code")
            },
        })
    }

    return (
        <div className="space-y-6">
            <div className="text-center">
                <h1 className="text-2xl font-bold text-white">Welcome</h1>
                <p className="text-gray-400 mt-2">Enter the household access code</p>
            </div>
            <form onSubmit={handleSubmit} className="space-y-4">
                <div>
                    <input
                        type="password"
                        value={accessCode}
                        onChange={e => setAccessCode(e.target.value)}
                        placeholder="Access code"
                        className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white text-center text-lg tracking-widest focus:outline-none focus:border-brand-500"
                        required
                    />
                </div>
                <label className="flex items-center justify-center gap-2 text-sm text-gray-300">
                    <input
                        type="checkbox"
                        checked={rememberMe}
                        onChange={e => setRememberMe(e.target.checked)}
                        className="rounded border-gray-700 bg-gray-900 text-brand-500 focus:ring-brand-500"
                    />
                    Remember me for 30 days
                </label>
                {error && <p className="text-red-400 text-sm text-center">{error}</p>}
                <button
                    type="submit"
                    disabled={isPending}
                    className="w-full py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                >
                    {isPending ? "Verifying..." : "Continue"}
                </button>
            </form>
            <div className="text-center">
                <button
                    onClick={() => window.location.href = "/login"}
                    className="text-sm text-gray-400 hover:text-white"
                >
                    Admin login
                </button>
            </div>
        </div>
    )
}
