import type { ErrorData } from "hls.js"

export function formatHlsFatalErrorMessage(error: ErrorData): string {
    return `HLS error: ${error.error?.message || error.details}`
}
