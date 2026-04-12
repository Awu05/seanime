import { useAuthSetup, useAuthSetupCheck } from "@/api/hooks/auth.hooks"
import { SettingsCard } from "@/app/(main)/settings/_components/settings-card"
import { SettingsPageHeader } from "@/app/(main)/settings/_components/settings-card"
import { Alert } from "@/components/ui/alert"
import { Switch } from "@/components/ui/switch"
import React from "react"
import { LuUsers } from "react-icons/lu"
import { RiSettings3Fill } from "react-icons/ri"
import { toast } from "sonner"

export function DenshiSettings() {

    const [settings, setSettings] = React.useState<DenshiSettings | null>(null)
    const settingsRef = React.useRef<DenshiSettings | null>(null)
    const [loading, setLoading] = React.useState(true)

    React.useEffect(() => {
        if (window.electron?.denshiSettings) {
            window.electron.denshiSettings.get().then((s) => {
                setSettings(s)
                settingsRef.current = s
                setLoading(false)
            })
        }
    }, [])

    function updateSetting(key: keyof DenshiSettings, value: boolean | string) {
        if (!settingsRef.current || !window.electron?.denshiSettings) return

        const newSettings = { ...settingsRef.current, [key]: value }
        settingsRef.current = newSettings
        setSettings(newSettings)
        window.electron.denshiSettings.set(newSettings)
    }

    if (loading || !settings) {
        return null
    }

    return (
        <div className="space-y-4">
            <SettingsCard title="Window">
                <Switch
                    side="right"
                    value={settings.minimizeToTray}
                    onValueChange={(v) => updateSetting("minimizeToTray", v)}
                    label="Minimize to tray on close"
                    help="When enabled, closing the window will minimize the app to the system tray instead of quitting."
                />
                <Switch
                    side="right"
                    value={settings.openInBackground}
                    onValueChange={(v) => updateSetting("openInBackground", v)}
                    label="Open in background"
                    help="When enabled, the app will start hidden. You can show it from the system tray."
                />
            </SettingsCard>

            <SettingsCard title="System">
                <Switch
                    side="right"
                    value={settings.openAtLaunch}
                    onValueChange={(v) => updateSetting("openAtLaunch", v)}
                    label="Open at launch"
                    help={window.electron?.platform === "linux"
                        ? "This feature is not supported on Linux."
                        : "When enabled, the app will start automatically when you log in to your computer."}
                    disabled={window.electron?.platform === "linux"}
                />
            </SettingsCard>

            <DenshiMultiUserSetup />

            <div className="flex items-center gap-2 text-sm text-gray-500 bg-gray-50 dark:bg-gray-900/30 rounded-lg p-3 border border-gray-200 dark:border-gray-800 border-dashed">
                <RiSettings3Fill className="text-base" />
                <span>Settings are saved automatically and applied after a restart</span>
            </div>
        </div>
    )
}

function DenshiMultiUserSetup() {
    const { data: setupCheck, isLoading } = useAuthSetupCheck()
    const { mutate: setup, isPending } = useAuthSetup()

    const [username, setUsername] = React.useState("")
    const [password, setPassword] = React.useState("")
    const [accessCode, setAccessCode] = React.useState("")
    const [error, setError] = React.useState("")
    const [showForm, setShowForm] = React.useState(false)

    if (isLoading) return null

    const isMultiUser = setupCheck?.multiUser === true

    if (isMultiUser) {
        return (
            <>
                <SettingsPageHeader
                    title="Profiles"
                    description="Multi-user profiles are enabled"
                    icon={LuUsers}
                />
                <SettingsCard>
                    <Alert
                        intent="success"
                        description="Multi-user mode is active. Manage profiles from the Profiles tab in settings."
                    />
                </SettingsCard>
            </>
        )
    }

    function handleSubmit(e: React.FormEvent) {
        e.preventDefault()
        setError("")
        if (!username.trim() || !password.trim()) {
            setError("Username and password are required")
            return
        }
        setup({ username: username.trim(), password: password.trim(), accessCode: accessCode.trim() || undefined }, {
            onSuccess: () => {
                toast.success("Multi-user profiles enabled. The app will reload.")
                setTimeout(() => window.location.reload(), 1000)
            },
            onError: () => {
                setError("Failed to set up profiles")
            },
        })
    }

    return (
        <>
            <SettingsPageHeader
                title="Profiles"
                description="Enable multi-user profiles for your household"
                icon={LuUsers}
            />
            <SettingsCard>
                {!showForm ? (
                    <div className="space-y-3">
                        <p className="text-sm text-gray-400">
                            Enable profiles to let multiple people use Seanime with their own AniList accounts,
                            watch history, and settings. Each person can stream torrents simultaneously.
                        </p>
                        <button
                            type="button"
                            onClick={() => setShowForm(true)}
                            className="px-4 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium transition-colors"
                        >
                            Set Up Profiles
                        </button>
                    </div>
                ) : (
                    <form onSubmit={handleSubmit} className="space-y-4">
                        <p className="text-sm text-gray-400">
                            Create an admin account. You'll use these credentials to log in and manage profiles.
                        </p>
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
                            <p className="text-xs text-gray-500 mb-1">Other household members enter this to access the profile picker</p>
                            <input
                                type="text"
                                value={accessCode}
                                onChange={e => setAccessCode(e.target.value)}
                                className="w-full px-3 py-2 bg-gray-900 border border-gray-700 rounded-lg text-white focus:outline-none focus:border-brand-500"
                            />
                        </div>
                        {error && <p className="text-red-400 text-sm">{error}</p>}
                        <div className="flex gap-2">
                            <button
                                type="submit"
                                disabled={isPending}
                                className="px-4 py-2 bg-brand-500 hover:bg-brand-600 text-white rounded-lg font-medium disabled:opacity-50"
                            >
                                {isPending ? "Setting up..." : "Enable Profiles"}
                            </button>
                            <button
                                type="button"
                                onClick={() => setShowForm(false)}
                                className="px-4 py-2 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg font-medium"
                            >
                                Cancel
                            </button>
                        </div>
                    </form>
                )}
            </SettingsCard>
        </>
    )
}
