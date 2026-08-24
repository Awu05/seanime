/**
 * Dispatches enqueued items in bounded-size batches spaced apart by intervalMs, instead of all
 * at once. Used for translation requests: selecting a translated subtitle track can enqueue
 * hundreds of dialogue lines at once, and firing one request per line immediately competes with
 * playback buffering right when the user starts watching.
 */
export class ThrottledDispatcher<T> {
    private queue: T[] = []
    private scheduled = false

    constructor(
        private readonly dispatch: (item: T) => void,
        private readonly batchSize: number,
        private readonly intervalMs: number,
        private readonly scheduleNext: (cb: () => void, ms: number) => void = (cb, ms) => setTimeout(cb, ms),
    ) {
    }

    enqueue(item: T): void {
        this.queue.push(item)
        if (!this.scheduled) {
            this.scheduled = true
            this.scheduleNext(() => this.drain(), 0)
        }
    }

    private drain(): void {
        this.scheduled = false
        const batch = this.queue.splice(0, this.batchSize)
        for (const item of batch) {
            this.dispatch(item)
        }
        if (this.queue.length > 0) {
            this.scheduled = true
            this.scheduleNext(() => this.drain(), this.intervalMs)
        }
    }
}
