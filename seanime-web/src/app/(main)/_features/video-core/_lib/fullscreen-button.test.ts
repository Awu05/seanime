import { describe, expect, it } from "vitest"
import { fullscreenButtonAction, windowFillsScreen } from "./fullscreen-button"

describe("windowFillsScreen", () => {
    it("is true when the window covers the screen, allowing for rounding", () => {
        expect(windowFillsScreen(960, 540, { width: 960, height: 540 })).toBe(true)
        expect(windowFillsScreen(959, 539, { width: 960, height: 540 })).toBe(true)
    })

    it("is false when browser UI or a taskbar takes up part of the screen", () => {
        expect(windowFillsScreen(1920, 960, { width: 1920, height: 1080 })).toBe(false)
        expect(windowFillsScreen(1280, 1080, { width: 1920, height: 1080 })).toBe(false)
    })
})

describe("fullscreenButtonAction", () => {
    it("switches between the mini player and the full player when the window already fills the screen", () => {
        expect(fullscreenButtonAction({ isMiniPlayer: true, isFullscreen: false, fillsScreen: true })).toBe("expand")
        expect(fullscreenButtonAction({ isMiniPlayer: false, isFullscreen: false, fillsScreen: true })).toBe("mini-player")
    })

    it("uses real fullscreen when the window doesn't fill the screen", () => {
        expect(fullscreenButtonAction({ isMiniPlayer: true, isFullscreen: false, fillsScreen: false })).toBe("expand-fullscreen")
        expect(fullscreenButtonAction({ isMiniPlayer: false, isFullscreen: false, fillsScreen: false })).toBe("enter-fullscreen")
    })

    it("exits real fullscreen first when it is on", () => {
        expect(fullscreenButtonAction({ isMiniPlayer: false, isFullscreen: true, fillsScreen: true })).toBe("exit-fullscreen")
    })
})
