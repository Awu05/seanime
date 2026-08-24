export type SeaErrorLike = {
    code?: string
    response?: {
        status?: number
        data?: unknown
    }
} | null | undefined

export function _handleSeaError(error: SeaErrorLike): string {
    if (!error) return "Unknown error"

    if (!error.response) {
        if (error.code === "ECONNABORTED") {
            return "Request timed out. Please try again."
        }
        return "Could not reach the server. Please check your connection and try again."
    }

    const { status, data } = error.response

    if (typeof data === "string" && data.trim()) {
        return "Server Error: " + data
    }

    const err = (data as { error?: string } | null | undefined)?.error

    if (!err) {
        return `Server Error${status ? ` (${status})` : ""}: No details provided`
    }

    if (err.includes("Too many requests")) {
        return "AniList: Too many requests, please wait a moment and try again."
    }

    try {
        const graphqlErr = JSON.parse(err) as any
        if (graphqlErr.graphqlErrors && graphqlErr.graphqlErrors.length > 0 && !!graphqlErr.graphqlErrors[0]?.message) {
            return "AniList error: " + graphqlErr.graphqlErrors[0]?.message
        }
        return "AniList error"
    }
    catch (e) {
        if (err.includes("no cached data") || err.includes("cache lookup failed")) {
            return ""
        }
        return "Error: " + err
    }
}
