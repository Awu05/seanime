import { describe, expect, it } from "vitest"
import { shouldApplyNativePlayerEvent } from "./native-player-lifecycle"

describe("shouldApplyNativePlayerEvent", () => {
    it("always applies open-and-await, even after termination - it starts a new stream lifecycle", () => {
        expect(shouldApplyNativePlayerEvent("open-and-await", true)).toBe(true)
        expect(shouldApplyNativePlayerEvent("open-and-await", false)).toBe(true)
    })

    it("applies watch when the player has not been terminated", () => {
        expect(shouldApplyNativePlayerEvent("watch", false)).toBe(true)
    })

    it("ignores a watch that arrives after the player was terminated", () => {
        expect(shouldApplyNativePlayerEvent("watch", true)).toBe(false)
    })

    it("ignores an abort-open that arrives after the player was terminated", () => {
        expect(shouldApplyNativePlayerEvent("abort-open", true)).toBe(false)
    })

    it("applies abort-open when the player has not been terminated", () => {
        expect(shouldApplyNativePlayerEvent("abort-open", false)).toBe(true)
    })
})
