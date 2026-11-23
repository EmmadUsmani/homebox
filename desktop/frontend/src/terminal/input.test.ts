import { describe, expect, test } from "vitest";
import { inputChunkEnd } from "./input";

describe("terminal input chunking", () => {
    test("sends short input intact", () => {
        expect(inputChunkEnd("echo hello\r")).toBe(11);
    });

    test("bounds long ASCII input", () => {
        expect(inputChunkEnd("a".repeat(5000))).toBe(4096);
    });

    test("preserves a surrogate pair across the chunk boundary", () => {
        const input = "a".repeat(4095) + "🧪";
        const end = inputChunkEnd(input);
        expect(end).toBe(4095);
        expect(input.slice(end)).toBe("🧪");
    });
});
