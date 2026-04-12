import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { useServerMutation } from "@/api/client/requests"

export function useCreateProfile() {
    return useServerMutation<
        any,
        { name: string; avatar?: string; pin?: string }
    >({
        endpoint: API_ENDPOINTS.ADMIN_PROFILES.CreateProfile.endpoint,
        method: API_ENDPOINTS.ADMIN_PROFILES.CreateProfile.methods[0],
        mutationKey: [API_ENDPOINTS.ADMIN_PROFILES.CreateProfile.key],
    })
}

export function useDeleteProfile() {
    return useServerMutation<
        { success: boolean },
        { id: string }
    >({
        endpoint: API_ENDPOINTS.ADMIN_PROFILES.DeleteProfile.endpoint,
        method: "POST",
        mutationKey: [API_ENDPOINTS.ADMIN_PROFILES.DeleteProfile.key],
    })
}

export function useSetInstanceAccessCode() {
    return useServerMutation<
        { success: boolean },
        { accessCode: string }
    >({
        endpoint: API_ENDPOINTS.ADMIN_PROFILES.SetAccessCode.endpoint,
        method: API_ENDPOINTS.ADMIN_PROFILES.SetAccessCode.methods[0],
        mutationKey: [API_ENDPOINTS.ADMIN_PROFILES.SetAccessCode.key],
    })
}
