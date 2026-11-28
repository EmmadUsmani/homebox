import { isMac } from "../platform";
import { useEffect, useState, type FormEvent } from "react";
import { Clipboard } from "@wailsio/runtime";
import type { RemoteState, RemoteVM } from "./useRemoteHosts";
import "./remote.css";

const hints = {
    unreachable: ["Unreachable", "The name doesn’t resolve or the connection timed out.", isMac ? "The Mac may be off, asleep or off this network." : "The host may be off, asleep or off this network."],
    "not-running": ["VM not running", isMac ? "The Mac refused the connection." : "The host refused the connection.", "Box’s SSH relay listens only while the VM runs, so the VM may be stopped."],
    "connection-broken": ["Connection broken", "The SSH connection failed.", "The VM’s SSH server may still be booting, or your login may need attention."],
    reachable: ["Reachable", "Logged in with a pinned key.", "Guest status is available over the laptop’s SSH login."],
    "host-key-changed": ["Host key changed", "", ""],
} satisfies Record<RemoteState, string[]>;

export function AddRemoteHost({ add }: { add: (alias: string) => Promise<void> }) {
    const [open, setOpen] = useState(false);
    const [alias, setAlias] = useState("");
    const [error, setError] = useState("");
    const [saving, setSaving] = useState(false);

    async function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        setSaving(true);
        setError("");
        try {
            await add(alias.trim());
            setAlias("");
            setOpen(false);
        } catch (cause) {
            setError(String(cause));
        } finally {
            setSaving(false);
        }
    }

    return <>
        <button type="button" className="add-host" aria-expanded={open} onClick={() => { setError(""); setOpen(!open); }}>Add a host</button>
        {open && <form className="remote-add-form" onSubmit={(event) => void submit(event)}>
            <label htmlFor="remote-alias">SSH alias</label>
            <input autoFocus id="remote-alias" value={alias} maxLength={64} required
                onChange={(event) => setAlias(event.target.value)} />
            <small>Use an alias from your SSH config with a pinned key.</small>
            {error && <small role="alert">{error}</small>}
            <button type="submit" disabled={saving}>Add host</button>
        </form>}
    </>;
}

export function RemoteView({ vm, checkAgain }: { vm: RemoteVM; checkAgain: () => void }) {
    const [copyResult, setCopyResult] = useState("");
    const [now, setNow] = useState(Date.now);
    useEffect(() => {
        const timer = window.setInterval(() => setNow(Date.now()), 1_000);
        return () => window.clearInterval(timer);
    }, []);
    const state = vm.check?.state;
    const changed = state === "host-key-changed";
    const checked = vm.check && Math.max(0, Math.floor((now - Date.parse(vm.check.checkedAt)) / 1_000));
    const command = `box repin ${vm.name}`;
    const retry = <button type="button" disabled={vm.checking} onClick={checkAgain}>Check again</button>;
    const source = vm.host ? `on ${vm.host} · from your SSH config` : "From your SSH config";

    async function copy() {
        try {
            await Clipboard.SetText(command);
            setCopyResult("Command copied");
        } catch {
            setCopyResult("Could not copy");
        }
    }

    return <section className="remote-view" aria-label="Remote VM">
        <header className="remote-header">
            <h1>{vm.name}</h1>
            <span>{source}</span>
            <span className="remote-age">{vm.checking ? "Checking…" : checked !== undefined ? `Checked ${checked} s ago` : ""}</span>
        </header>
        <div className="remote-content">
            {changed && <section className="remote-key-alert" role="alert">
                <h2><Dot state={state} />Host key changed: not connecting</h2>
                <p>SSH couldn’t verify this VM against its pinned key. The VM may have been rebuilt, its pin may be missing, or something else may be answering in its place.</p>
                <p>If you rebuilt it on purpose, review and replace the keys in Settings on that {isMac ? "Mac" : "host"}, or run <code>{command}</code> there using the VM’s name. Then reinstall the laptop SSH config.</p>
                <div className="remote-actions">
                    <button type="button" onClick={() => void copy()}>Copy command</button>
                    {retry}
                    <small role="status">{copyResult}</small>
                </div>
            </section>}
            {!changed && <div className="remote-actions">
                <strong>{state ? hints[state][0] : vm.error ?? "Checking SSH connection"}</strong>
                {retry}
            </div>}
            <h2 className="remote-subtitle">{changed ? (isMac ? "Other states the app can show for a VM on another Mac" : "Other states the app can show for a VM on another host") : "Remote connection hints"}</h2>
            <div className="remote-grid">
                {(["unreachable", "not-running", "connection-broken", "reachable"] as const).map((hint) =>
                    <article className="card remote-hint" key={hint} aria-current={state === hint ? "true" : undefined}>
                        <h3><Dot state={hint} />{hints[hint][0]}</h3>
                        <p>{hints[hint][1]}</p>
                        <small>{hints[hint][2]}</small>
                    </article>
                )}
            </div>
            <small>Manage remote VMs on their {isMac ? "host Mac" : "host"}. A refused connection can mean the VM is stopped or has been deleted.</small>
        </div>
    </section>;
}

function Dot({ state }: { state?: RemoteState }) {
    return <span className="remote-dot" data-state={state} aria-hidden="true" />;
}
