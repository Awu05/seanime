export type FullscreenButtonAction = "expand" | "expand-fullscreen" | "mini-player" | "enter-fullscreen" | "exit-fullscreen"

// Whether the browser window already covers the whole screen (like TV Bro on Android TV), where
// real fullscreen changes nothing visible. Allows a couple of pixels for rounding.
export function windowFillsScreen(width: number, height: number, screen: { width: number, height: number }) {
    return width >= screen.width - 2 && height >= screen.height - 2
}

// What the fullscreen button does. When the window already fills the screen, it switches between
// the mini player and the full player instead, since real fullscreen would look like nothing happened.
export function fullscreenButtonAction(state: { isMiniPlayer: boolean, isFullscreen: boolean, fillsScreen: boolean }): FullscreenButtonAction {
    if (state.isMiniPlayer) return state.fillsScreen ? "expand" : "expand-fullscreen"
    if (state.isFullscreen) return "exit-fullscreen"
    return state.fillsScreen ? "mini-player" : "enter-fullscreen"
}
