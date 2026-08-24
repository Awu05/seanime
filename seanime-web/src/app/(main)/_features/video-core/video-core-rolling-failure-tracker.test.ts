import { describe, expect, it } from "vitest"
import { RollingFailureTracker } from "./video-core-rolling-failure-tracker"

describe("RollingFailureTracker", () => {
    it("does not trip below the threshold", () => {
        let now = 0
        const tracker = new RollingFailureTracker(60_000, 4, () => now)

        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
    })

    it("trips once the threshold is reached within the window", () => {
        let now = 0
        const tracker = new RollingFailureTracker(60_000, 4, () => now)

        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(true)
    })

    it("does not trip if failures are spread out beyond the window", () => {
        let now = 0
        const tracker = new RollingFailureTracker(60_000, 4, () => now)

        expect(tracker.recordFailure()).toBe(false)
        now += 30_000
        expect(tracker.recordFailure()).toBe(false)
        now += 30_001 // first failure is now outside the 60s window
        expect(tracker.recordFailure()).toBe(false)
        now += 30_000
        expect(tracker.recordFailure()).toBe(false)
    })

    it("is not fooled by successes interspersed between failures - only reset() clears it", () => {
        let now = 0
        const tracker = new RollingFailureTracker(60_000, 4, () => now)

        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
        // A "success" in caller code just means recordFailure() isn't called - it does not
        // implicitly forgive the failures already recorded.
        expect(tracker.recordFailure()).toBe(true)
    })

    it("reset() clears recorded failures", () => {
        let now = 0
        const tracker = new RollingFailureTracker(60_000, 4, () => now)

        tracker.recordFailure()
        tracker.recordFailure()
        tracker.recordFailure()
        tracker.reset()

        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
        expect(tracker.recordFailure()).toBe(false)
    })
})
