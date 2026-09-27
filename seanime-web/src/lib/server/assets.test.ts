import { describe, expect, it, vi } from "vitest"

vi.mock("@/api/client/server-url", () => ({
    getServerBaseUrl: () => "http://server:43211",
}))

import { cachedImageUrl, getImageUrl } from "./assets"

const base = "http://server:43211"

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

describe("getImageUrl", () => {
    it("keeps offline assets on their own route", () => {
        expect(getImageUrl("{{LOCAL_ASSETS}}/1/cover.jpg")).toBe(`${base}/offline-assets/1/cover.jpg`)
    })

    it("routes outside images", () => {
        expect(getImageUrl("https://s4.anilist.co/cover.jpg")).toContain(`${base}/api/v1/image-cache?url=`)
    })
})
