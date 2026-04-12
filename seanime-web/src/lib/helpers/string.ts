/**
 * Strip HTML tags from a string and decode common entities.
 * AniList descriptions contain <br>, <i>, <b>, etc.
 */
export function stripHtml(html: string): string {
    if (!html) return ""
    // Remove tags
    let text = html.replace(/<[^>]*>/g, "")
    // Decode common entities
    text = text
        .replace(/&lt;/g, "<")
        .replace(/&gt;/g, ">")
        .replace(/&quot;/g, "\"")
        .replace(/&#39;/g, "'")
        .replace(/&nbsp;/g, " ")
        .replace(/&amp;/g, "&")
    return text.trim()
}
