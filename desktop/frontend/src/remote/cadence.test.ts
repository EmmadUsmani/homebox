import { expect, test } from "vitest";
import { nextRemoteInterval } from "./cadence";

test("backs off failed checks and resets the interval after a successful check", () => {
    let interval = 0;
    for (const expected of [15_000, 30_000, 60_000, 120_000, 120_000]) {
        interval = nextRemoteInterval(interval, false);
        expect(interval).toBe(expected);
    }
    expect(nextRemoteInterval(interval, true)).toBe(15_000);
    expect(nextRemoteInterval(15_000, false)).toBe(30_000);
});
