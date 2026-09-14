import { useServerMutation, useServerQuery } from "@/api/client/requests"
import { TerminateProfileStream_Variables } from "@/api/generated/endpoint.types"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { AdminActivitySnapshot } from "@/api/generated/types"
import { useQueryClient } from "@tanstack/react-query"

export function useGetAdminActivity(enabled: boolean) {
    return useServerQuery<AdminActivitySnapshot>({
        endpoint: API_ENDPOINTS.ADMIN_ACTIVITY.GetAdminActivity.endpoint,
        method: API_ENDPOINTS.ADMIN_ACTIVITY.GetAdminActivity.methods[0],
        queryKey: [API_ENDPOINTS.ADMIN_ACTIVITY.GetAdminActivity.key],
        refetchInterval: 5000,
        gcTime: 0,
        enabled,
    })
}

export function useTerminateProfileStream() {
    const queryClient = useQueryClient()

    return useServerMutation<boolean, TerminateProfileStream_Variables>({
        endpoint: API_ENDPOINTS.ADMIN_ACTIVITY.TerminateProfileStream.endpoint,
        method: API_ENDPOINTS.ADMIN_ACTIVITY.TerminateProfileStream.methods[0],
        mutationKey: [API_ENDPOINTS.ADMIN_ACTIVITY.TerminateProfileStream.key],
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.ADMIN_ACTIVITY.GetAdminActivity.key] })
        },
    })
}
