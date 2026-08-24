import { describe, expect, it } from "vitest"
import { buildDirectstreamAttachmentUrl } from "./directstream-attachment-url"

describe("buildDirectstreamAttachmentUrl", () => {
    // Regression test: the backend keys active directstream sessions by clientId
    // (internal/directstream/serve.go ServeEchoAttachments), but attachment URLs never carried
    // one, so it always fell back to the literal string "default" - which never matches a real
    // client and reproduces the "no stream for client" error seen on every attachment request.
    // The clientId is embedded in playbackInfo.streamUrl by the backend
    // (see the StreamUrl construction in internal/directstream/*.go), so it's extracted from
    // there rather than invented on the frontend.
    it("includes the clientId extracted from the stream URL", () => {
        const url = buildDirectstreamAttachmentUrl({
            baseUrl: "http://localhost:43211",
            filename: "Roboto-Medium.ttf",
            streamUrl: "http://localhost:43211/api/v1/directstream/stream?id=abc&clientId=00836a44-f261-4245-bfe0-5a91641cebfb",
            tokenQuery: "",
        })

        expect(url).toBe(
            "http://localhost:43211/api/v1/directstream/att/Roboto-Medium.ttf?clientId=00836a44-f261-4245-bfe0-5a91641cebfb",
        )
    })

    it("appends clientId after an existing token query with &", () => {
        const url = buildDirectstreamAttachmentUrl({
            baseUrl: "http://localhost:43211",
            filename: "arial.ttf",
            streamUrl: "http://localhost:43211/api/v1/directstream/stream?id=abc&clientId=client-1",
            tokenQuery: "?token=xyz",
        })

        expect(url).toBe("http://localhost:43211/api/v1/directstream/att/arial.ttf?token=xyz&clientId=client-1")
    })

    it("URL-encodes the filename", () => {
        const url = buildDirectstreamAttachmentUrl({
            baseUrl: "http://localhost:43211",
            filename: "My Font.ttf",
            streamUrl: "http://localhost:43211/api/v1/directstream/stream?id=abc&clientId=client-1",
            tokenQuery: "",
        })

        expect(url).toBe("http://localhost:43211/api/v1/directstream/att/My%20Font.ttf?clientId=client-1")
    })

    it("omits clientId when the stream URL has none", () => {
        const url = buildDirectstreamAttachmentUrl({
            baseUrl: "http://localhost:43211",
            filename: "arial.ttf",
            streamUrl: "http://localhost:43211/api/v1/directstream/stream?id=abc",
            tokenQuery: "",
        })

        expect(url).toBe("http://localhost:43211/api/v1/directstream/att/arial.ttf")
    })

    it("omits clientId when the stream URL is missing", () => {
        const url = buildDirectstreamAttachmentUrl({
            baseUrl: "http://localhost:43211",
            filename: "arial.ttf",
            streamUrl: undefined,
            tokenQuery: "",
        })

        expect(url).toBe("http://localhost:43211/api/v1/directstream/att/arial.ttf")
    })
})
