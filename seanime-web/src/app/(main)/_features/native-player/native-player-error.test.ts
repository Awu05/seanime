import type { ErrorData } from "hls.js"
import { describe, expect, it } from "vitest"
import { formatHlsFatalErrorMessage } from "./native-player-error"

describe("formatHlsFatalErrorMessage", () => {
    it("uses the underlying error's message when present", () => {
        const data = { error: { message: "network timeout" }, details: "fragLoadError" } as ErrorData
        expect(formatHlsFatalErrorMessage(data)).toBe("HLS error: network timeout")
    })

    it("falls back to the error details when the error has no message", () => {
        const data = { error: {}, details: "fragLoadError" } as ErrorData
        expect(formatHlsFatalErrorMessage(data)).toBe("HLS error: fragLoadError")
    })

    it("falls back to the error details when error itself is missing", () => {
        const data = { details: "manifestLoadError" } as ErrorData
        expect(formatHlsFatalErrorMessage(data)).toBe("HLS error: manifestLoadError")
    })
})
