export function appendLogs<T>(previous: T[], incoming: T[], limit = 5000): T[] {
    return previous.concat(incoming).slice(-limit);
}
