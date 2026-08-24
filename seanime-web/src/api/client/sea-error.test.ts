import { describe, expect, it } from "vitest"
import { _handleSeaError } from "./sea-error"

describe("_handleSeaError", () => {
    it("reports a network error when there is no response at all", () => {
        expect(_handleSeaError({ response: undefined })).toBe(
            "Could not reach the server. Please check your connection and try again.",
        )
    })

    it("reports a timeout distinctly from a generic network error", () => {
        expect(_handleSeaError({ code: "ECONNABORTED", response: undefined })).toBe(
            "Request timed out. Please try again.",
        )
    })

    it("includes the raw string body when the server responds with plain text", () => {
        expect(_handleSeaError({ response: { status: 500, data: "boom" } })).toBe(
            "Server Error: boom",
        )
    })

    it("includes the HTTP status when the response has no error field", () => {
        expect(_handleSeaError({ response: { status: 502, data: {} } })).toBe(
            "Server Error (502): No details provided",
        )
    })

    it("surfaces a friendly message for AniList rate limiting", () => {
        expect(_handleSeaError({
            response: { status: 429, data: { error: "Too many requests, retry later" } },
        })).toBe("AniList: Too many requests, please wait a moment and try again.")
    })

    it("extracts the message from an AniList GraphQL error payload", () => {
        const graphqlErr = JSON.stringify({ graphqlErrors: [{ message: "Media not found" }] })
        expect(_handleSeaError({
            response: { status: 400, data: { error: graphqlErr } },
        })).toBe("AniList error: Media not found")
    })

    it("passes through a plain backend error message", () => {
        expect(_handleSeaError({
            response: { status: 400, data: { error: "invalid profile id" } },
        })).toBe("Error: invalid profile id")
    })

    it("mutes cache-miss errors since they are not user-actionable", () => {
        expect(_handleSeaError({
            response: { status: 404, data: { error: "no cached data available" } },
        })).toBe("")
    })
})
