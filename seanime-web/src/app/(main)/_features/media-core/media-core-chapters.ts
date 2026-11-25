export type MediaCoreChapter = {
    label: string | null
    start: number
    end: number
}

export function getChapterType(name: string | null | undefined) {
    if (!name) return false
    if (/opening$|^opening\s|^op$/mi.test(name)) return "Opening"
    if (/ending$|^ending\s|^ed$|^credits/mi.test(name)) return "Ending"
    if (/^intro$|recap/mi.test(name)) return "Intro"
    if (/^outro$/mi.test(name)) return "Outro"
    return false
}

export function introIsOpening(chapters: Array<{ label: string | null }>) {
    const types = chapters.map(chapter => getChapterType(chapter.label)).filter(Boolean)
    return types.includes("Intro") && !types.includes("Opening")
}

type SkipOptions = {
    guardIntro?: boolean
}

// TV openings run about 90 seconds. An "Intro" chapter outside this range is more likely a cold
// open with story in it, so it isn't skipped as the opening.
const OPENING_MIN_SECONDS = 60
const OPENING_MAX_SECONDS = 135

function isOpeningLength(chapter: { start?: number, end?: number }) {
    if (typeof chapter.start !== "number" || typeof chapter.end !== "number") return false
    const length = chapter.end - chapter.start
    return length >= OPENING_MIN_SECONDS && length <= OPENING_MAX_SECONDS
}

export function getDefaultSkipChapters<T extends { label: string | null, start?: number, end?: number }>(chapters: T[],
    options: SkipOptions = {},
) {
    let opening: T | null = null
    let ending: T | null = null

    for (const chapter of chapters) {
        const type = getChapterType(chapter.label)
        if (!opening && type === "Opening") opening = chapter
        if (!ending && type === "Ending") ending = chapter
        if (opening && ending) break
    }

    // Files that label the opening "Intro" have no "Opening" chapter. Only an opening-length Intro
    // is taken as the opening, and this never affects the ending.
    if (!opening && options.guardIntro !== false && introIsOpening(chapters)) {
        opening = chapters.find(chapter => getChapterType(chapter.label) === "Intro" && isOpeningLength(chapter)) ?? null
    }

    return { opening, ending }
}

function getPatterns(value: string) {
    return value
        .split(",")
        .map(pattern => pattern.trim())
        .filter(Boolean)
}

function getRegexes(value: string) {
    return getPatterns(value).flatMap(pattern => {
        try {
            return [new RegExp(pattern, "i")]
        }
        catch {
            return []
        }
    })
}

export function getSkipPatternError(value: string) {
    for (const pattern of getPatterns(value)) {
        try {
            new RegExp(pattern, "i")
        }
        catch {
            return `Invalid regex: ${pattern}`
        }
    }
    return ""
}

export function getSkipChapters<T extends { label: string | null, start?: number, end?: number }>(chapters: T[], patterns: string, options: SkipOptions = {}) {
    const defaults = getDefaultSkipChapters(chapters, options)
    const regexes = getRegexes(patterns)

    return chapters.filter(chapter => {
        if (chapter === defaults.opening || chapter === defaults.ending) return true
        const label = chapter.label
        if (!label) return false
        return regexes.some(regex => regex.test(label))
    })
}

// nextAutoSkip skips each chapter once per visit: browsers like TV Bro keep reporting the old position
// until a seek finishes, and seeking again on every update would restart it forever.
export function nextAutoSkip(chapter: MediaCoreChapter, lastSkippedEnd: number | null) {
    if (chapter.end === lastSkippedEnd) return { seekTo: null, lastSkippedEnd }
    return { seekTo: chapter.end, lastSkippedEnd: chapter.end }
}

export function getSkipLabel(name: string | null) {
    const type = getChapterType(name)
    if (type === "Opening" || type === "Ending") return type
    return name?.trim() || "Chapter"
}
