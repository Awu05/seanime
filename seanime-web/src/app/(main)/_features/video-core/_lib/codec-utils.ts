export function checkCodecSupport(
    codec: string,
    options: {
        isMobile: boolean
        canUseMatroskaFallback: boolean
        canPlayType: (codec: string) => "probably" | "maybe" | ""
    },
): boolean {
    if (!codec) return false
    if (options.isMobile) return false

    const isMatroska = codec.startsWith("video/x-matroska") || codec.startsWith("video/matroska")
    if (options.canPlayType(codec) === "probably") {
        return true
    }

    if (isMatroska && options.canUseMatroskaFallback) {
        const container = codec.startsWith("video/x-matroska") ? "video/x-matroska" : "video/matroska"
        const mp4 = replaceMimeContainer(codec, container, "video/mp4")
        const webm = replaceMimeContainer(codec, container, "video/webm")
        return options.canPlayType(mp4) === "probably" || options.canPlayType(webm) === "probably"
    }

    return false
}

// KNOWN_PROBLEM_CODECS lists codecs known to commonly lack browser support (e.g. HEVC requires a
// license most browsers don't ship), each paired with a representative MIME codec string to probe
// via HTMLMediaElement.canPlayType. Only flagged as unsupported on a definite "no" (empty string)
// - "maybe" is treated as playable to avoid wrongly penalizing browsers with real support
// (e.g. Safari, or Chromium with OS/hardware HEVC decode) during torrent auto-select.
const KNOWN_PROBLEM_CODECS: { name: string, mimeCodec: string }[] = [
    { name: "HEVC", mimeCodec: "video/mp4; codecs=\"hvc1.1.6.L93.B0\"" },
]

export function getUnsupportedVideoCodecs(canPlayType: (codec: string) => "probably" | "maybe" | ""): string[] {
    return KNOWN_PROBLEM_CODECS
        .filter(({ mimeCodec }) => canPlayType(mimeCodec) === "")
        .map(({ name }) => name)
}

function replaceMimeContainer(codec: string, from: string, to: string): string {
    if (codec.startsWith(from)) {
        return to + codec.substring(from.length)
    }
    return codec
}
