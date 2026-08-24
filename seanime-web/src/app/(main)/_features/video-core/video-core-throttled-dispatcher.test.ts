import { describe, expect, it, vi } from "vitest"
import { ThrottledDispatcher } from "./video-core-throttled-dispatcher"

function makeFakeScheduler() {
    const pending: (() => void)[] = []
    const calls: number[] = []
    const scheduleNext = (cb: () => void, ms: number) => {
        calls.push(ms)
        pending.push(cb)
    }
    const runNext = () => {
        const cb = pending.shift()
        if (!cb) throw new Error("nothing scheduled")
        cb()
    }
    return { scheduleNext, runNext, calls, pendingCount: () => pending.length }
}

describe("ThrottledDispatcher", () => {
    it("dispatches a batch of items up to batchSize once the schedule tick fires", () => {
        const dispatch = vi.fn()
        const { scheduleNext, runNext } = makeFakeScheduler()
        const d = new ThrottledDispatcher<string>(dispatch, 3, 250, scheduleNext)

        d.enqueue("a")
        d.enqueue("b")
        expect(dispatch).not.toHaveBeenCalled() // nothing dispatched until the scheduled tick runs

        runNext()
        expect(dispatch).toHaveBeenCalledTimes(2)
        expect(dispatch).toHaveBeenNthCalledWith(1, "a")
        expect(dispatch).toHaveBeenNthCalledWith(2, "b")
    })

    it("splits more than batchSize items across multiple ticks spaced by intervalMs", () => {
        const dispatch = vi.fn()
        const { scheduleNext, runNext, calls } = makeFakeScheduler()
        const d = new ThrottledDispatcher<string>(dispatch, 2, 250, scheduleNext)

        for (const item of ["a", "b", "c", "d", "e"]) d.enqueue(item)

        runNext() // first batch: dispatched "immediately" (0ms tick)
        expect(dispatch).toHaveBeenCalledTimes(2)
        expect(calls[0]).toBe(0)

        runNext() // second batch: after the throttle interval
        expect(dispatch).toHaveBeenCalledTimes(4)
        expect(calls[1]).toBe(250)

        runNext() // final, partial batch
        expect(dispatch).toHaveBeenCalledTimes(5)
    })

    it("does not schedule a redundant tick while one is already pending", () => {
        const dispatch = vi.fn()
        const { scheduleNext, runNext, calls } = makeFakeScheduler()
        const d = new ThrottledDispatcher<string>(dispatch, 10, 250, scheduleNext)

        d.enqueue("a")
        d.enqueue("b")
        d.enqueue("c")
        expect(calls).toHaveLength(1) // only one tick scheduled for all three enqueues

        runNext()
        expect(dispatch).toHaveBeenCalledTimes(3)
    })

    it("schedules a new tick for items enqueued after the queue has fully drained", () => {
        const dispatch = vi.fn()
        const { scheduleNext, runNext, calls } = makeFakeScheduler()
        const d = new ThrottledDispatcher<string>(dispatch, 10, 250, scheduleNext)

        d.enqueue("a")
        runNext()
        expect(dispatch).toHaveBeenCalledTimes(1)

        d.enqueue("b")
        expect(calls).toHaveLength(2)
        runNext()
        expect(dispatch).toHaveBeenCalledTimes(2)
    })
})
