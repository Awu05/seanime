const volumeKeys: Record<string, "up" | "down" | "mute"> = {
    AudioVolumeUp: "up",
    AudioVolumeDown: "down",
    AudioVolumeMute: "mute",
    // Older browsers' names for the same keys.
    VolumeUp: "up",
    VolumeDown: "down",
    VolumeMute: "mute",
}

// hardwareVolumeKey returns which volume key was pressed, if any. Android TV browsers report a
// remote's volume keys through key and leave code empty, so both are checked.
export function hardwareVolumeKey(e: Pick<KeyboardEvent, "key" | "code">): "up" | "down" | "mute" | null {
    return volumeKeys[e.key] ?? volumeKeys[e.code] ?? null
}
