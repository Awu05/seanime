import { useServerMutation, useServerQuery } from "@/api/client/requests"
import { API_ENDPOINTS } from "@/api/generated/endpoints"

export function useAuthSetupCheck() {
    return useServerQuery<{
        needsSetup: boolean
        hasAccessCode: boolean
        multiUser: boolean
        sidecar: boolean
    }>({
        endpoint: API_ENDPOINTS.AUTH.SetupCheck.endpoint,
        method: API_ENDPOINTS.AUTH.SetupCheck.methods[0],
        queryKey: [API_ENDPOINTS.AUTH.SetupCheck.key],
        enabled: true,
    })
}

export function useAuthSetup() {
    return useServerMutation<
        { success: boolean },
        { username: string; password: string; accessCode?: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.Setup.endpoint,
        method: API_ENDPOINTS.AUTH.Setup.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.Setup.key],
    })
}

export function useAuthAdminLogin() {
    return useServerMutation<
        { token: string; profile: any },
        { username: string; password: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.AdminLogin.endpoint,
        method: API_ENDPOINTS.AUTH.AdminLogin.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.AdminLogin.key],
    })
}

export function useAuthAccessCode() {
    return useServerMutation<
        { token: string },
        { accessCode: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.AccessCode.endpoint,
        method: API_ENDPOINTS.AUTH.AccessCode.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.AccessCode.key],
    })
}

export function useAuthGetProfiles() {
    return useServerQuery<any[]>({
        endpoint: API_ENDPOINTS.AUTH.GetProfiles.endpoint,
        method: API_ENDPOINTS.AUTH.GetProfiles.methods[0],
        queryKey: [API_ENDPOINTS.AUTH.GetProfiles.key],
        enabled: true,
    })
}

export function useAuthSelectProfile() {
    return useServerMutation<
        { token: string; profile: any },
        { profileId: string; pin?: string }
    >({
        endpoint: API_ENDPOINTS.AUTH.SelectProfile.endpoint,
        method: API_ENDPOINTS.AUTH.SelectProfile.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.SelectProfile.key],
    })
}

export function useAuthMe() {
    return useServerQuery<{
        profile?: any
        isAdmin: boolean
        scope: string
    }>({
        endpoint: API_ENDPOINTS.AUTH.Me.endpoint,
        method: API_ENDPOINTS.AUTH.Me.methods[0],
        queryKey: [API_ENDPOINTS.AUTH.Me.key],
        enabled: true,
    })
}

export function useAuthLogout() {
    return useServerMutation<{ success: boolean }, void>({
        endpoint: API_ENDPOINTS.AUTH.LogoutSession.endpoint,
        method: API_ENDPOINTS.AUTH.LogoutSession.methods[0],
        mutationKey: [API_ENDPOINTS.AUTH.LogoutSession.key],
    })
}
