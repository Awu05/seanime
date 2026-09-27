import { afterEach, describe, expect, it, vi } from "vitest"

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

describe("getImageUrl", () => {
    it("keeps offline assets on their own route", () => {
        expect(getImageUrl("{{LOCAL_ASSETS}}/1/cover.jpg")).toBe(`${base}/offline-assets/1/cover.jpg`)
    })

    it("routes outside images", () => {
        expect(getImageUrl("https://s4.anilist.co/cover.jpg")).toContain(`${base}/api/v1/image-cache?url=`)
    })
})
