import { useEffect, useRef, useState } from "react";

export function poll(task: () => Promise<void>, interval: number) {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async () => {
        try {
            await task();
        } finally {
            if (!stopped) timer = setTimeout(tick, interval);
        }
    };
    void tick();
    return () => {
        stopped = true;
        clearTimeout(timer);
    };
}

export function usePolling<T>(
    key: string | undefined,
    read: (key: string) => Promise<T>,
    revision = 0,
    merge?: (previous: T | undefined, next: T) => T,
) {
    const [result, setResult] = useState<{ key: string; data?: T; error?: string }>();
    const pending = useRef<Promise<void>>(Promise.resolve());

    useEffect(() => {
        if (key === undefined) return;
        let active = true;
        const stop = poll(() => {
            pending.current = pending.current.then(async () => {
                if (!active) return;
                try {
                    const data = await read(key);
                    if (active) {
                        setResult((previous) => ({
                            key,
                            data: merge ? merge(previous?.key === key ? previous.data : undefined, data) : data,
                        }));
                    }
                } catch (error) {
                    if (active) {
                        setResult((previous) => ({
                            key, data: previous?.key === key ? previous.data : undefined, error: String(error),
                        }));
                    }
                }
            });
            return pending.current;
        }, 3000);
        return () => { active = false; stop(); };
    }, [key, read, revision, merge]);

    return result?.key === key ? result : undefined;
}
