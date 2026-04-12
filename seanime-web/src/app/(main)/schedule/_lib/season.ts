import { AL_MediaSeason } from "@/api/generated/types"

export type SeasonKind = "previous" | "current" | "next"

// Maps 0-indexed month to a base season.
// Jan(0)-Feb(1)-Mar(2) = WINTER
// Apr(3)-May(4)-Jun(5) = SPRING
// Jul(6)-Aug(7)-Sep(8) = SUMMER
// Oct(9)-Nov(10)-Dec(11) = FALL
function monthToSeasonIndex(month: number): number {
    return Math.floor(month / 3) // 0=WINTER, 1=SPRING, 2=SUMMER, 3=FALL
}

const SEASONS: AL_MediaSeason[] = ["WINTER", "SPRING", "SUMMER", "FALL"]

export function computeSeasonParams(
    kind: SeasonKind,
    now: Date = new Date(),
): { season: AL_MediaSeason; seasonYear: number } {
    const month = now.getMonth() // 0-11
    const year = now.getFullYear()

    let idx = monthToSeasonIndex(month)
    let y = year

    if (kind === "previous") {
        idx -= 1
        if (idx < 0) {
            idx = 3 // FALL
            y -= 1
        }
    } else if (kind === "next") {
        idx += 1
        if (idx > 3) {
            idx = 0 // WINTER
            y += 1
        }
    }

    return { season: SEASONS[idx], seasonYear: y }
}

export function formatSeasonLabel(season: AL_MediaSeason, year: number): string {
    const pretty: Record<AL_MediaSeason, string> = {
        WINTER: "Winter",
        SPRING: "Spring",
        SUMMER: "Summer",
        FALL: "Fall",
    }
    return `${pretty[season] ?? season} ${year}`
}
