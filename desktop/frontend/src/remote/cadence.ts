const interval = 15_000;
const maximum = 120_000;

export function nextRemoteInterval(previous: number, reachable: boolean): number {
    return reachable ? interval : Math.min(maximum, Math.max(interval, previous * 2));
}
