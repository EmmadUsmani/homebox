import { afterEach, describe, expect, it, vi } from "vitest";
import { poll } from "./usePolling";

afterEach(() => vi.useRealTimers());

describe("poll", () => {
    it("waits for each request to settle before scheduling another", async () => {
        vi.useFakeTimers();
        let complete!: () => void;
        const task = vi.fn(() => new Promise<void>((resolve) => { complete = resolve; }));
        const stop = poll(task, 3000);
        await vi.advanceTimersByTimeAsync(9000);
        expect(task).toHaveBeenCalledTimes(1);
        complete();
        await vi.advanceTimersByTimeAsync(2999);
        expect(task).toHaveBeenCalledTimes(1);
        await vi.advanceTimersByTimeAsync(1);
        expect(task).toHaveBeenCalledTimes(2);
        stop();
        complete();
        await vi.advanceTimersByTimeAsync(9000);
        expect(task).toHaveBeenCalledTimes(2);
    });
});
