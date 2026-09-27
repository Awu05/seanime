import { getServerBaseUrl } from "@/api/client/server-url"
import { SERVER_AUTH_TOKEN_STORAGE_KEY } from "@/app/(main)/_atoms/server-status.atoms"
import { createServerPasswordHMACAuth } from "@/lib/server/hmac-auth"

const IMAGE_CACHE_PATH = "/api/v1/image-cache"

// The server password's hash is both the app's HMAC secret and only ever stored once a password
// was actually entered, so finding it here also means "a server password protects this instance".
// Read directly (rather than through the jotai atom) since cachedImageUrl runs synchronously in
// plain <img src> builders that aren't hooks.
function getStoredServerPasswordHash(): string | undefined {
    try {
        const raw = localStorage.getItem(SERVER_AUTH_TOKEN_STORAGE_KEY)
        if (!raw) return undefined
        const value = JSON.parse(raw)
        return typeof value === "string" && value !== "" ? value : undefined
    }
    catch {
        return undefined
    }
}

// Routes an outside image through the server's image cache, which keeps a copy on disk so the
// image still shows during an internet outage. Anything else is returned unchanged, including
// URLs already on the server, so applying it twice is harmless. When a server password is set, an
// HMAC token is attached the same way the manga reader authenticates /api/v1/image-proxy, since an
// <img> load can't send the X-Seanime-Token header the server would otherwise require.
export function cachedImageUrl(path: string, serverBaseUrl: string): string {
    if (!/^https?:\/\//i.test(path) || path.startsWith(`${serverBaseUrl}/`)) {
        return path
    }
    const url = `${serverBaseUrl}${IMAGE_CACHE_PATH}?url=${encodeURIComponent(path)}`
    const passwordHash = getStoredServerPasswordHash()
    if (!passwordHash) {
        return url
    }
    return `${url}${createServerPasswordHMACAuth(passwordHash).generateQueryParamSync(IMAGE_CACHE_PATH, "&")}`
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
