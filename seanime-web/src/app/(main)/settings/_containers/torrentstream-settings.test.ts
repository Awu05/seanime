import { describe, expect, it } from "vitest"
import { torrentstreamSchema } from "./torrentstream-settings.schema"


const validBase = {
    enabled: true,
    downloadDir: "",
    autoSelect: true,
    disableIPV6: false,
    addToLibrary: false,
    torrentClientHost: "",
    preferredResolution: "-",
    includeInLibrary: false,
}

describe("torrentstreamSchema", () => {
    it("clearing torrentClientPort falls back to the default port instead of failing validation", () => {
        // Field.Number sends `undefined` when the input is cleared - the help text says
        // "Leave empty for default. Default is 43213.", so an undefined port must validate and
        // resolve to 43213, not block the save.
        const result = torrentstreamSchema.safeParse({ ...validBase, torrentClientPort: undefined })
        expect(result.success).toBe(true)
        if (result.success) {
            expect(result.data.torrentClientPort).toBe(43213)
        }
    })

    it("still accepts an explicit custom port", () => {
        const result = torrentstreamSchema.safeParse({ ...validBase, torrentClientPort: 51413 })
        expect(result.success).toBe(true)
        if (result.success) {
            expect(result.data.torrentClientPort).toBe(51413)
        }
    })
})
