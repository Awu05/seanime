import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/api/client/server-url", () => ({
    getServerBaseUrl: () => "http://server:43211",
}))

import { SERVER_AUTH_TOKEN_STORAGE_KEY } from "@/app/(main)/_atoms/server-status.atoms"
import { cachedImageUrl, getImageUrl } from "./assets"

const base = "http://server:43211"

// Mirrors how serverAuthTokenAtom (atomWithStorage) persists the password hash, so cachedImageUrl's
// direct localStorage read finds it the same way the app would have written it.
function stubServerPassword(hash: string | undefined) {
    const store = new Map<string, string>()
    if (hash !== undefined) store.set(SERVER_AUTH_TOKEN_STORAGE_KEY, JSON.stringify(hash))
    vi.stubGlobal("localStorage", {
        getItem: (key: string) => store.get(key) ?? null,
    })
}

afterEach(() => {
    vi.unstubAllGlobals()
})

describe("cachedImageUrl", () => {
    it("routes outside images through the image cache", () => {
        expect(cachedImageUrl("https://s4.anilist.co/file/cover.jpg?x=1&y=2", base))
            .toBe(`${base}/api/v1/image-cache?url=${encodeURIComponent("https://s4.anilist.co/file/cover.jpg?x=1&y=2")}`)
        expect(cachedImageUrl("http://artworks.thetvdb.com/a.jpg", base)).toContain("/api/v1/image-cache?url=")
    })

    it("leaves local, data and blob sources alone", () => {
        for (const src of ["/no-cover.png", "", "data:image/png;base64,AAAA", "blob:http://server/1"]) {
            expect(cachedImageUrl(src, base)).toBe(src)
        }
    })

    it("leaves URLs on the Seanime server alone, so it never wraps twice", () => {
        const once = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        expect(cachedImageUrl(once, base)).toBe(once)
        expect(cachedImageUrl(`${base}/offline-assets/1/cover.jpg`, base)).toBe(`${base}/offline-assets/1/cover.jpg`)
    })
})

describe("cachedImageUrl with a server password", () => {
    it("attaches an HMAC token so an <img> load authenticates, like /api/v1/image-proxy does", () => {
        stubServerPassword("abc123hash")
        const url = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        expect(url).toMatch(new RegExp(`^${base}/api/v1/image-cache\\?url=[^&]+&token=.+$`))
    })

    it("still never wraps twice once a token is attached", () => {
        stubServerPassword("abc123hash")
        const once = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        expect(cachedImageUrl(once, base)).toBe(once)
    })

    it("omits the token when no password is stored", () => {
        stubServerPassword(undefined)
        expect(cachedImageUrl("https://s4.anilist.co/cover.jpg", base)).not.toContain("token=")
    })
})

describe("cachedImageUrl token caching", () => {
    beforeEach(() => {
        vi.useFakeTimers()
        vi.setSystemTime(new Date("2026-01-01T00:00:00Z"))
    })

    afterEach(() => {
        vi.useRealTimers()
    })

    // Each test gets its own fresh copy of the module, so the module-level token cache doesn't leak
    // between tests (or from the tests above, which share the statically imported module).
    async function freshCachedImageUrl() {
        vi.resetModules()
        const mod = await import("./assets")
        return mod.cachedImageUrl
    }

    it("returns the same URL for calls a few seconds apart", async () => {
        stubServerPassword("abc123hash")
        const cachedImageUrl = await freshCachedImageUrl()
        const first = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        vi.advanceTimersByTime(5_000)
        const second = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        expect(second).toBe(first)
    })

    it("regenerates the token once it's within an hour of the 24h expiry", async () => {
        stubServerPassword("abc123hash")
        const cachedImageUrl = await freshCachedImageUrl()
        const first = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        vi.advanceTimersByTime(23 * 60 * 60 * 1000 + 60 * 1000) // 23h1m: within the 1h refresh window
        const second = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        expect(second).not.toBe(first)
    })

    it("regenerates immediately when the stored password hash changes", async () => {
        stubServerPassword("abc123hash")
        const cachedImageUrl = await freshCachedImageUrl()
        const first = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        stubServerPassword("a-different-hash")
        const second = cachedImageUrl("https://s4.anilist.co/cover.jpg", base)
        expect(second).not.toBe(first)
    })
})

describe("getImageUrl", () => {
    it("keeps offline assets on their own route", () => {
        expect(getImageUrl("{{LOCAL_ASSETS}}/1/cover.jpg")).toBe(`${base}/offline-assets/1/cover.jpg`)
    })

    it("routes outside images", () => {
        expect(getImageUrl("https://s4.anilist.co/cover.jpg")).toContain(`${base}/api/v1/image-cache?url=`)
    })
})
