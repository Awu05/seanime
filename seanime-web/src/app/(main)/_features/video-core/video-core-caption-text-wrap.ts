/**
 * Word-wraps text to fit within maxWidth, using an injected measurer instead of a real canvas
 * context - keeps this pure and testable, and lets callers cache the result per (text, maxWidth)
 * instead of re-measuring every word on every animation frame a cue stays active.
 */
export function wrapCaptionText(text: string, maxWidth: number, measureWidth: (s: string) => number): string[] {
    const words = text.split(" ")
    const lines: string[] = []
    let currentLine = ""

    for (const word of words) {
        const testLine = currentLine ? `${currentLine} ${word}` : word
        const width = measureWidth(testLine)

        if (width > maxWidth && currentLine) {
            lines.push(currentLine)
            currentLine = word
        } else {
            currentLine = testLine
        }
    }
    if (currentLine) {
        lines.push(currentLine)
    }

    return lines
}
