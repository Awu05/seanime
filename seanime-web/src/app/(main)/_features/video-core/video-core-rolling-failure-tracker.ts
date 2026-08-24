/**
 * Tracks whether too many failures have happened within a rolling time window, regardless of
 * how many successes happened in between. A simple "reset the counter on any success" scheme
 * lets a chronically flaky source (e.g. a torrent stream with intermittent connectivity) oscillate
 * indefinitely between near-failures and micro-recoveries without ever tripping a ceiling, since
 * each individual recovery episode resets its own counter before the next one starts. This
 * tracker only forgets a failure once it ages out of the window - never because something else
 * succeeded in between.
 */
export class RollingFailureTracker {
    private timestamps: number[] = []

    constructor(
        private readonly windowMs: number,
        private readonly threshold: number,
        private readonly now: () => number = Date.now,
    ) {
    }

    /** Records a failure and returns whether the threshold has been reached within the window. */
    recordFailure(): boolean {
        const now = this.now()
        this.timestamps.push(now)
        this.timestamps = this.timestamps.filter(t => now - t <= this.windowMs)
        return this.timestamps.length >= this.threshold
    }

    reset(): void {
        this.timestamps = []
    }
}
