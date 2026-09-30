import { describe, expect, it } from "vitest"
import { hardwareVolumeKey } from "./volume-keys"

describe("hardwareVolumeKey", () => {
    it("recognizes volume keys that Android TV browsers report with an empty code", () => {
        expect(hardwareVolumeKey({ key: "AudioVolumeUp", code: "" })).toBe("up")
        expect(hardwareVolumeKey({ key: "AudioVolumeDown", code: "" })).toBe("down")
        expect(hardwareVolumeKey({ key: "AudioVolumeMute", code: "" })).toBe("mute")
    })

    it("recognizes the older key names and keyboard codes", () => {
        expect(hardwareVolumeKey({ key: "VolumeUp", code: "" })).toBe("up")
        expect(hardwareVolumeKey({ key: "Unidentified", code: "AudioVolumeDown" })).toBe("down")
    })

    it("ignores every other key", () => {
        expect(hardwareVolumeKey({ key: "ArrowUp", code: "ArrowUp" })).toBeNull()
    })
})
