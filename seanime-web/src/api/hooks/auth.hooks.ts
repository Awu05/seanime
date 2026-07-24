import { useServerMutation, useServerQuery } from "@/api/client/requests"
import { Login_Variables } from "@/api/generated/endpoint.types"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { Status } from "@/api/generated/types"
import { useSetServerStatus } from "@/app/(main)/_hooks/use-server-status"
import { useRouter } from "@/lib/navigation"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

// Query key for useAuthMe — kept in sync with the hook below so mutations
// that change the session (login/logout/select-profile) can invalidate it.
const AUTH_ME_KEY = [API_ENDPOINTS.USER_AUTH.GetMe.key]

export function useLogin() {
    const queryClient = useQueryClient()
    const router = useRouter()
    const setServerStatus = useSetServerStatus()

    return useServerMutation<Status, Login_Variables>({
        endpoint: API_ENDPOINTS.AUTH.Login.endpoint,
        method: API_ENDPOINTS.AUTH.Login.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.Login.key],
        onSuccess: async data => {
            if (data) {
                toast.success("Successfully authenticated")
                await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANIME_COLLECTION.GetLibraryCollection.key] })
                await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANILIST.GetRawAnimeCollection.key] })
                await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANILIST.GetAnimeCollection.key] })
                await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.MANGA.GetMangaCollection.key] })
                setServerStatus(data)
                router.push("/")
                queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANIME_ENTRIES.GetMissingEpisodes.key] })
                queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANIME_ENTRIES.GetAnimeEntry.key] })
                queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.MANGA.GetMangaEntry.key] })
            }
        },
        onError: async error => {
            toast.error(error.message)
            router.push("/")
        },
    })
}

export function useLogout() {
    const queryClient = useQueryClient()
    const router = useRouter()
    const setServerStatus = useSetServerStatus()

    return useServerMutation<Status>({
        endpoint: API_ENDPOINTS.AUTH.Logout.endpoint,
        method: API_ENDPOINTS.AUTH.Logout.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.Logout.key],
        onSuccess: async () => {
            toast.success("Successfully logged out")
            await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANIME_COLLECTION.GetLibraryCollection.key] })
            await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANILIST.GetRawAnimeCollection.key] })
            await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANILIST.GetAnimeCollection.key] })
            await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.MANGA.GetMangaCollection.key] })
            router.push("/")
            queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANIME_ENTRIES.GetMissingEpisodes.key] })
            queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ANIME_ENTRIES.GetAnimeEntry.key] })
            queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.MANGA.GetMangaEntry.key] })
        },
    })
}

// Multi-user auth hooks

export function useAuthSetupCheck(enabled: boolean = true) {
    return useServerQuery<{
        needsSetup: boolean
        hasAccessCode: boolean
        multiUser: boolean
        sidecar: boolean
    }>({
        endpoint: API_ENDPOINTS.USER_AUTH.SetupCheck.endpoint,
        method: API_ENDPOINTS.USER_AUTH.SetupCheck.methods[0],
        queryKey: [API_ENDPOINTS.USER_AUTH.SetupCheck.key],
        enabled,
        // Once we know setup is/isn't needed for this session, there's no need
        // to re-check on every navigation.
        staleTime: Infinity,
        muteError: true,
    })
}

export function useAuthSetup() {
    return useServerMutation<
        { success: boolean },
        { username: string; password: string; confirmPassword: string; accessCode?: string }
    >({
        endpoint: API_ENDPOINTS.USER_AUTH.AdminSetup.endpoint,
        method: API_ENDPOINTS.USER_AUTH.AdminSetup.methods[0],
        mutationKey: [API_ENDPOINTS.USER_AUTH.AdminSetup.key],
    })
}

export function useAuthAdminLogin() {
    const queryClient = useQueryClient()
    return useServerMutation<
        { token: string; profile: any },
        { username: string; password: string }
    >({
        endpoint: API_ENDPOINTS.USER_AUTH.AdminLogin.endpoint,
        method: API_ENDPOINTS.USER_AUTH.AdminLogin.methods[0],
        mutationKey: [API_ENDPOINTS.USER_AUTH.AdminLogin.key],
        // Suppress the default error toast — the login page shows its own
        // inline error, and a wrong password isn't the "unexpected server
        // error" the global toast is meant for.
        onError: () => {},
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: AUTH_ME_KEY })
        },
    })
}

export function useAuthAccessCode() {
    return useServerMutation<
        { token: string },
        { accessCode: string }
    >({
        endpoint: API_ENDPOINTS.USER_AUTH.AccessCode.endpoint,
        method: API_ENDPOINTS.USER_AUTH.AccessCode.methods[0],
        mutationKey: [API_ENDPOINTS.USER_AUTH.AccessCode.key],
        onError: () => {},
    })
}

export function useAuthGetProfiles() {
    return useServerQuery<any[]>({
        endpoint: API_ENDPOINTS.USER_AUTH.GetProfiles.endpoint,
        method: API_ENDPOINTS.USER_AUTH.GetProfiles.methods[0],
        queryKey: [API_ENDPOINTS.USER_AUTH.GetProfiles.key],
        enabled: true,
    })
}

export function useAuthSelectProfile() {
    const queryClient = useQueryClient()
    return useServerMutation<
        { token: string; profile: any },
        { profileId: string; pin?: string }
    >({
        endpoint: API_ENDPOINTS.USER_AUTH.SelectProfile.endpoint,
        method: API_ENDPOINTS.USER_AUTH.SelectProfile.methods[0],
        mutationKey: [API_ENDPOINTS.USER_AUTH.SelectProfile.key],
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: AUTH_ME_KEY })
        },
    })
}

export function useAuthMe(enabled: boolean = true) {
    return useServerQuery<{
        profile?: any
        isAdmin: boolean
        scope: string
    }>({
        endpoint: API_ENDPOINTS.USER_AUTH.GetMe.endpoint,
        method: API_ENDPOINTS.USER_AUTH.GetMe.methods[0],
        queryKey: [API_ENDPOINTS.USER_AUTH.GetMe.key],
        enabled,
        // Re-checked only when explicitly invalidated (login/logout), not on
        // every navigation — the cookie-based session doesn't change otherwise.
        staleTime: Infinity,
        retry: false,
        muteError: true,
    })
}

export function useAuthLogout() {
    const queryClient = useQueryClient()
    return useServerMutation<{ success: boolean }, void>({
        endpoint: API_ENDPOINTS.USER_AUTH.LogoutAuth.endpoint,
        method: API_ENDPOINTS.USER_AUTH.LogoutAuth.methods[0],
        mutationKey: [API_ENDPOINTS.USER_AUTH.LogoutAuth.key],
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: AUTH_ME_KEY })
        },
    })
}

// Profile settings hooks

export function useGetProfileSettings() {
    return useServerQuery<{
        overrides: string
        merged: any
    }>({
        endpoint: API_ENDPOINTS.PROFILE_SETTINGS.GetProfileSettings.endpoint,
        method: API_ENDPOINTS.PROFILE_SETTINGS.GetProfileSettings.methods[0],
        queryKey: [API_ENDPOINTS.PROFILE_SETTINGS.GetProfileSettings.key],
        enabled: true,
    })
}

export function useSaveProfileSettings() {
    return useServerMutation<
        { success: boolean },
        { overrides: string }
    >({
        endpoint: API_ENDPOINTS.PROFILE_SETTINGS.SaveProfileSettings.endpoint,
        method: API_ENDPOINTS.PROFILE_SETTINGS.SaveProfileSettings.methods[0],
        mutationKey: [API_ENDPOINTS.PROFILE_SETTINGS.SaveProfileSettings.key],
    })
}
