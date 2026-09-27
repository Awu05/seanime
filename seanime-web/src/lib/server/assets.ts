import { getServerBaseUrl } from "@/api/client/server-url"
import { SERVER_AUTH_TOKEN_STORAGE_KEY } from "@/app/(main)/_atoms/server-status.atoms"
import { createServerPasswordHMACAuth, HMAC_TOKEN_TTL_SECONDS } from "@/lib/server/hmac-auth"

const IMAGE_CACHE_PATH = "/api/v1/image-cache"

// A stored hash means a server password protects this instance. Read directly (not the jotai atom)
// since cachedImageUrl runs synchronously in <img src> builders.
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

// A fresh token every call would change the URL (it embeds iat/exp) and defeat the browser's
// URL-keyed cache, so the query param is reused until it's within an hour of expiring.
const IMAGE_CACHE_TOKEN_TTL_MS = HMAC_TOKEN_TTL_SECONDS * 1000
const IMAGE_CACHE_TOKEN_REFRESH_WINDOW_MS = 60 * 60 * 1000

let cachedImageCacheToken: { hash: string; param: string; expiresAt: number } | undefined

function getImageCacheAuthParam(passwordHash: string): string {
    const now = Date.now()
    if (!cachedImageCacheToken
        || cachedImageCacheToken.hash !== passwordHash
        || cachedImageCacheToken.expiresAt - now <= IMAGE_CACHE_TOKEN_REFRESH_WINDOW_MS) {
        cachedImageCacheToken = {
            hash: passwordHash,
            param: createServerPasswordHMACAuth(passwordHash).generateQueryParamSync(IMAGE_CACHE_PATH, "&"),
            expiresAt: now + IMAGE_CACHE_TOKEN_TTL_MS,
        }
    }
    return cachedImageCacheToken.param
}

// An HMAC token is attached when a server password is set, since an <img> load can't send the
// X-Seanime-Token header the server would otherwise require (same approach as /api/v1/image-proxy).
export function cachedImageUrl(path: string, serverBaseUrl: string): string {
    if (!/^https?:\/\//i.test(path) || path.startsWith(`${serverBaseUrl}/`)) {
        return path
    }
    const url = `${serverBaseUrl}${IMAGE_CACHE_PATH}?url=${encodeURIComponent(path)}`
    const passwordHash = getStoredServerPasswordHash()
    if (!passwordHash) {
        return url
    }
    return `${url}${getImageCacheAuthParam(passwordHash)}`
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
