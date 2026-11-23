import { useCallback, useEffect, useRef, useState } from "react";
import { Call } from "@wailsio/runtime";
import type { VMDetail } from "../types";
import { nextRemoteInterval } from "./cadence";

export type RemoteState = "reachable" | "unreachable" | "not-running" | "connection-broken" | "host-key-changed";
export type RemoteHost = { id: string; name: string; host: string };
export type RemoteCheck = { state: RemoteState; checkedAt: string; status?: VMDetail };
export type RemoteVM = RemoteHost & { check?: RemoteCheck; checking: boolean; error?: string };

type PendingCheck = {
    interval: number;
    active: boolean;
    timer?: number;
};

export function useRemoteHosts() {
    const [hosts, setHosts] = useState<RemoteHost[]>([]);
    const [entries, setEntries] = useState<Record<string, Partial<RemoteVM>>>({});
    const [error, setError] = useState("");
    const pending = useRef(new Map<string, PendingCheck>());

    const load = useCallback(async () => {
        try {
            setHosts(await Call.ByName("main.Remote.Hosts") as RemoteHost[]);
            setError("");
        } catch {
            setError("Could not read remote hosts. Check your SSH config and desktop settings.");
        }
    }, []);

    const check = useCallback(async function check(id: string) {
        const item = pending.current.get(id);
        if (!item || item.active) return;
        window.clearTimeout(item.timer);
        item.active = true;
        setEntries((current) => ({ ...current, [id]: { ...current[id], checking: true } }));

        let entry: Partial<RemoteVM>;
        try {
            const result = await Call.ByName("main.Remote.Check", id) as RemoteCheck;
            entry = { checking: false, check: result };
        } catch {
            entry = { checking: false, error: "Could not check this SSH alias." };
        }
        if (pending.current.get(id) !== item) return;
        item.active = false;
        item.interval = nextRemoteInterval(item.interval, entry.check?.state === "reachable");
        item.timer = window.setTimeout(() => void check(id), item.interval);
        setEntries((current) => ({ ...current, [id]: entry }));
    }, []);

    useEffect(() => {
        void load();
        const focus = () => {
            void load();
            for (const id of pending.current.keys()) void check(id);
        };
        window.addEventListener("focus", focus);
        const scheduled = pending.current;
        return () => {
            window.removeEventListener("focus", focus);
            for (const item of scheduled.values()) window.clearTimeout(item.timer);
            scheduled.clear();
        };
    }, [check, load]);

    useEffect(() => {
        const ids = new Set(hosts.map(({ id }) => id));
        for (const [id, item] of pending.current) {
            if (ids.has(id)) continue;
            window.clearTimeout(item.timer);
            pending.current.delete(id);
        }
        for (const id of ids) {
            if (pending.current.has(id)) continue;
            pending.current.set(id, { interval: 0, active: false });
            void check(id);
        }
    }, [hosts, check]);

    async function add(alias: string) {
        await Call.ByName("main.Remote.AddHost", alias);
        await load();
    }

    const vms: RemoteVM[] = hosts.map((host) => ({ ...host, checking: true, ...entries[host.id] }));
    return { vms, error, check, add };
}
