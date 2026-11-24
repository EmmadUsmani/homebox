import { useLayoutEffect, useRef, useState } from "react";
import { Call } from "@wailsio/runtime";
import { usePolling } from "../usePolling";

type Operation = "Setup" | "Create" | "Provision" | "Delete";

export interface CreateOptions {
    Name: string;
    Profile: string;
    CPUs: number;
    MemoryGiB: number;
    DiskGiB: number;
}

export type Defaults = Omit<CreateOptions, "Name"> & { Profiles: string[] };

interface Progress {
    lines: string[] | null;
    step?: string;
    retry: boolean;
}

interface Job {
    operation: Operation;
    name: string;
    active: boolean;
    error?: string;
    progress?: Progress;
}

const ready = (): Promise<boolean> => Call.ByName("main.Lifecycle.HostReady");
const progress = (): Promise<Progress> => Call.ByName("main.Lifecycle.Progress");

export function useLifecycle(refresh: () => void, created: (name: string) => Promise<void>) {
    const [job, setJob] = useState<Job>();
    const [sequence, setSequence] = useState(0);
    const busy = useRef(false);
    const onCreated = useRef(created);
    useLayoutEffect(() => { onCreated.current = created; }, [created]);
    const host = usePolling("host", ready, sequence);
    const live = usePolling(job?.active ? String(sequence) : undefined, progress);

    async function run(operation: Operation, name = "", ...args: unknown[]) {
        if (busy.current) return;
        busy.current = true;
        setJob({ operation, name, active: true });
        setSequence((value) => value + 1);
        let error: string | undefined;
        try {
            await Call.ByName(`main.Lifecycle.${operation}`, ...args);
        } catch (failure) {
            error = String(failure);
        }

        const finished = await progress().catch(() => undefined);
        setJob({ operation, name, active: false, error, progress: finished });
        setSequence((value) => value + 1);
        busy.current = false;
        refresh();
        if (operation === "Create" && !error) await onCreated.current(name);
    }

    return {
        host,
        job: job?.active ? { ...job, progress: live?.data } : job,
        run,
        dismiss: () => setJob(undefined),
    };
}

export type LifecycleController = ReturnType<typeof useLifecycle>;
