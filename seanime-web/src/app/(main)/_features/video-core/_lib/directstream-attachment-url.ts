export function buildDirectstreamAttachmentUrl({ baseUrl, filename, streamUrl, tokenQuery }: {
    baseUrl: string
    filename: string
    streamUrl: string | undefined
    tokenQuery: string
}): string {
    const clientId = extractClientIdFromStreamUrl(streamUrl)

    let url = `${baseUrl}/api/v1/directstream/att/${encodeURIComponent(filename)}${tokenQuery}`
    if (clientId) {
        url += `${tokenQuery ? "&" : "?"}clientId=${encodeURIComponent(clientId)}`
    }
    return url
}

function extractClientIdFromStreamUrl(streamUrl: string | undefined): string | null {
    if (!streamUrl) return null
    const queryIndex = streamUrl.indexOf("?")
    if (queryIndex === -1) return null
    return new URLSearchParams(streamUrl.slice(queryIndex + 1)).get("clientId")
}
