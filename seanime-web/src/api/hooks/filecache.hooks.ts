import { useServerMutation, useServerQuery } from "@/api/client/requests"
import { RemoveFileCacheBucket_Variables, SetImageCacheLimit_Variables } from "@/api/generated/endpoint.types"
import { API_ENDPOINTS } from "@/api/generated/endpoints"
import { OfflineCopiesInfo } from "@/api/generated/types"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

export function useGetFileCacheTotalSize() {
    return useServerMutation<boolean>({
        endpoint: API_ENDPOINTS.FILECACHE.GetFileCacheTotalSize.endpoint,
        method: API_ENDPOINTS.FILECACHE.GetFileCacheTotalSize.methods[0],
        mutationKey: [API_ENDPOINTS.FILECACHE.GetFileCacheTotalSize.key],
    })
}

export function useRemoveFileCacheBucket(onSuccess?: () => void) {
    return useServerMutation<boolean, RemoveFileCacheBucket_Variables>({
        endpoint: API_ENDPOINTS.FILECACHE.RemoveFileCacheBucket.endpoint,
        method: API_ENDPOINTS.FILECACHE.RemoveFileCacheBucket.methods[0],
        mutationKey: [API_ENDPOINTS.FILECACHE.RemoveFileCacheBucket.key],
        onSuccess: async () => {
            toast.success("Cache cleared")
            onSuccess?.()
        },
    })
}

export function useGetFileCacheMediastreamVideoFilesTotalSize() {
    return useServerMutation<boolean>({
        endpoint: API_ENDPOINTS.FILECACHE.GetFileCacheMediastreamVideoFilesTotalSize.endpoint,
        method: API_ENDPOINTS.FILECACHE.GetFileCacheMediastreamVideoFilesTotalSize.methods[0],
        mutationKey: [API_ENDPOINTS.FILECACHE.GetFileCacheMediastreamVideoFilesTotalSize.key],
    })
}

export function useClearFileCacheMediastreamVideoFiles(onSuccess?: () => void) {
    return useServerMutation<boolean>({
        endpoint: API_ENDPOINTS.FILECACHE.ClearFileCacheMediastreamVideoFiles.endpoint,
        method: API_ENDPOINTS.FILECACHE.ClearFileCacheMediastreamVideoFiles.methods[0],
        mutationKey: [API_ENDPOINTS.FILECACHE.ClearFileCacheMediastreamVideoFiles.key],
        onSuccess: async () => {
            toast.success("Cache cleared")
            onSuccess?.()
        },
    })
}

export function useGetOfflineCopies() {
    return useServerQuery<OfflineCopiesInfo>({
        endpoint: API_ENDPOINTS.FILECACHE.GetOfflineCopies.endpoint,
        method: API_ENDPOINTS.FILECACHE.GetOfflineCopies.methods[0],
        queryKey: [API_ENDPOINTS.FILECACHE.GetOfflineCopies.key],
    })
}

export function useSetImageCacheLimit() {
    const queryClient = useQueryClient()
    return useServerMutation<boolean, SetImageCacheLimit_Variables>({
        endpoint: API_ENDPOINTS.FILECACHE.SetImageCacheLimit.endpoint,
        method: API_ENDPOINTS.FILECACHE.SetImageCacheLimit.methods[0],
        mutationKey: [API_ENDPOINTS.FILECACHE.SetImageCacheLimit.key],
        onSuccess: async () => {
            toast.success("Image cache limit saved")
            await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.FILECACHE.GetOfflineCopies.key] })
        },
    })
}

export function useClearOfflineCopies() {
    const queryClient = useQueryClient()
    return useServerMutation<boolean>({
        endpoint: API_ENDPOINTS.FILECACHE.ClearOfflineCopies.endpoint,
        method: API_ENDPOINTS.FILECACHE.ClearOfflineCopies.methods[0],
        mutationKey: [API_ENDPOINTS.FILECACHE.ClearOfflineCopies.key],
        onSuccess: async () => {
            toast.success("Saved title info and images cleared")
            await queryClient.invalidateQueries({ queryKey: [API_ENDPOINTS.FILECACHE.GetOfflineCopies.key] })
        },
    })
}
