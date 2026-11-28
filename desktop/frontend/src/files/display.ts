export function visibleText(text: string) {
    return text.replace(/[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/gu, (character) =>
        "⟦U+" + character.codePointAt(0)!.toString(16).toUpperCase().padStart(4, "0") + "⟧");
}
