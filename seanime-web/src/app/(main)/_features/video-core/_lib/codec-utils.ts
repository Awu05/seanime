import { isChromiumBased } from "@/lib/utils/browser-detection"

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

// KNOWN_PROBLEM_CODECS lists codecs common in anime releases that many browsers/devices can't
// decode (HEVC needs a license most browsers don't ship; 10-bit H.264 and AV1 lack hardware
// decoders on many TV devices), each paired with a representative MIME codec string to probe via
// HTMLMediaElement.canPlayType. Names must match the server's knownProblemVideoCodecs
// (internal/torrents/autoselect/comparison.go). Only flagged as unsupported on a definite "no"
// (empty string) - "maybe" is treated as playable to avoid wrongly penalizing browsers with real
// support (e.g. Safari, or Chromium with OS/hardware HEVC decode) during torrent auto-select.
const KNOWN_PROBLEM_CODECS: { name: string, mimeCodec: string }[] = [
    { name: "HEVC", mimeCodec: "video/mp4; codecs=\"hvc1.1.6.L93.B0\"" },
    { name: "Hi10P", mimeCodec: "video/mp4; codecs=\"avc1.6E0028\"" },
    { name: "AV1", mimeCodec: "video/mp4; codecs=\"av01.0.08M.10\"" },
]

export function getUnsupportedVideoCodecs(canPlayType: (codec: string) => "probably" | "maybe" | ""): string[] {
    return KNOWN_PROBLEM_CODECS
        .filter(({ mimeCodec }) => canPlayType(mimeCodec) === "")
        .map(({ name }) => name)
}

// isMatroskaUnsupported reports whether the browser can't play MKV files, the format most anime
// releases use. Chromium plays H.264/AAC MKVs even when canPlayType says otherwise; other browsers
// (Firefox, Safari) are asked, so one that gains MKV support isn't flagged.
export function isMatroskaUnsupported(options: {
    isChromium: boolean
    canPlayType: (codec: string) => "probably" | "maybe" | ""
}): boolean {
    return !options.isChromium && options.canPlayType("video/x-matroska; codecs=\"avc1.640028, mp4a.40.2\"") === ""
}

// browserCannotPlayMatroska is isMatroskaUnsupported for the current browser.
export function browserCannotPlayMatroska(): boolean {
    const video = document.createElement("video")
    return isMatroskaUnsupported({
        isChromium: isChromiumBased(),
        canPlayType: codec => video.canPlayType(codec) as "probably" | "maybe" | "",
    })
}

// matroskaPlaybackError explains why streamPath can't play, or returns null when it can. Firefox
// accepts MKV served as video/webm but can't decode it, so it waits without an error.
export function matroskaPlaybackError(streamPath: string | undefined, matroskaUnsupported: boolean): string | null {
    if (!matroskaUnsupported || !streamPath?.toLowerCase().endsWith(".mkv")) return null
    return "This browser can't play MKV videos. Use Chrome, Edge, Opera or the Seanime desktop app, or use an external player like VLC or MPV."
}

const LEARNED_UNSUPPORTED_CODECS_KEY = "sea-learned-unsupported-video-codecs"
// A learned codec can be a false positive (the same load error also covers non-codec failures like
// a broken stream response), so it expires instead of penalizing the codec on this device forever.
const LEARNED_CODEC_TTL_MS = 30 * 24 * 60 * 60 * 1000

function readLearnedCodecs(): Record<string, number> {
    try {
        const parsed: unknown = JSON.parse(localStorage.getItem(LEARNED_UNSUPPORTED_CODECS_KEY) ?? "{}")
        return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, number> : {}
    }
    catch {
        return {}
    }
}

// Codecs this device actually failed to decode during playback. canPlayType alone isn't reliable:
// Android WebView, for instance, can report 10-bit H.264 as playable when the TV's hardware
// decoder can't handle it.
export function getLearnedUnsupportedVideoCodecs(now = Date.now()): string[] {
    return Object.entries(readLearnedCodecs())
        .filter(([, learnedAt]) => typeof learnedAt === "number" && now - learnedAt < LEARNED_CODEC_TTL_MS)
        .map(([codec]) => codec)
}

export function learnUnsupportedVideoCodecs(codecs: string[], now = Date.now()) {
    if (!codecs.length) return
    try {
        const learned = readLearnedCodecs()
        for (const codec of codecs) learned[codec] = now
        localStorage.setItem(LEARNED_UNSUPPORTED_CODECS_KEY, JSON.stringify(learned))
    }
    catch {
    }
}

function replaceMimeContainer(codec: string, from: string, to: string): string {
    if (codec.startsWith(from)) {
        return to + codec.substring(from.length)
    }
    return codec
}
