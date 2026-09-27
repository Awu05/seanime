import { getServerBaseUrl } from "@/api/client/server-url"

const IMAGE_CACHE_PATH = "/api/v1/image-cache"

// Routes an outside image through the server's image cache, which keeps a copy on disk so the
// image still shows during an internet outage. Anything else is returned unchanged, including
// URLs already on the server, so applying it twice is harmless.
export function cachedImageUrl(path: string, serverBaseUrl: string): string {
    if (!/^https?:\/\//i.test(path) || path.startsWith(`${serverBaseUrl}/`)) {
        return path
    }
    return `${serverBaseUrl}${IMAGE_CACHE_PATH}?url=${encodeURIComponent(path)}`
}

export function getImageUrl(path: string) {
    if (path.startsWith("{{LOCAL_ASSETS}}")) {
        return `${getServerBaseUrl()}/${path.replace("{{LOCAL_ASSETS}}", "offline-assets")}`
    }

    return cachedImageUrl(path, getServerBaseUrl())
}

export function getAssetUrl(path: string) {
    let p = path.replaceAll("\\", "/")

    if (p.startsWith("/")) {
        p = p.substring(1)
    }

    p = encodeURIComponent(p).replace(/\(/g, "%28").replace(/\)/g, "%29")

    if (p.startsWith("{{LOCAL_ASSETS}}")) {
        return `${getServerBaseUrl()}/${p.replace("{{LOCAL_ASSETS}}", "offline-assets")}`
    }

    return `${getServerBaseUrl()}/assets/${p}`
}

export function legacy_getAssetUrl(path: string) {
    let p = path.replaceAll("\\", "/")

    if (p.startsWith("/")) {
        p = p.substring(1)
    }

    p = encodeURIComponent(p).replace(/\(/g, "%28").replace(/\)/g, "%29")

    return `${getServerBaseUrl()}/assets/${p}`
}
