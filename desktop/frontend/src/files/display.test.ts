import { describe, expect, it } from "vitest";
import { visibleText } from "./display";

describe("visible file text", () => {
    it("shows control, bidi, zero-width and line separator code points", () => {
        expect(visibleText("a\x1b[31m\u202etxt\u200b\n\t\u2028\u2029\u{E0001}"))
            .toBe("a⟦U+001B⟧[31m⟦U+202E⟧txt⟦U+200B⟧⟦U+000A⟧⟦U+0009⟧⟦U+2028⟧⟦U+2029⟧⟦U+E0001⟧");
    });

    it("keeps printable names intact for React's text rendering", () => {
        for (const name of ["-dash", "..lookalike", "<img src=x>", "{{7*7}}", "https:example.invalid",
            "mixed-pаypal-🚀", "x".repeat(255)]) {
            expect(visibleText(name)).toBe(name);
        }
    });
});
