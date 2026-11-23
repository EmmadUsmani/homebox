// Keep each binding payload small and preserve UTF-16 surrogate pairs.
export function inputChunkEnd(input: string): number {
    const limit = 4096;
    if (input.length <= limit) return input.length;
    return /[\uD800-\uDBFF]/.test(input.charAt(limit - 1)) ? limit - 1 : limit;
}
