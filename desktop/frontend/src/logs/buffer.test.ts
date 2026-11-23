import { expect, it } from "vitest";
import { appendLogs } from "./buffer";

it("keeps the latest 5,000 log entries in arrival order", () => {
    const history = Array.from({ length: 4999 }, (_, index) => index);
    const lines = appendLogs(history, [4999, 5000]);
    expect(lines).toEqual([...history.slice(1), 4999, 5000]);
    expect(history).toHaveLength(4999);
});
