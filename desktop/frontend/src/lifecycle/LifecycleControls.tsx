import { type FormEvent, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import { Call } from "@wailsio/runtime";
import type { CreateOptions, Defaults, LifecycleController } from "./useLifecycle";
import "./lifecycle.css";

function Dialog({ title, close, children }: { title: string; close: () => void; children: ReactNode }) {
    const dialog = useRef<HTMLDialogElement>(null);
    useLayoutEffect(() => {
        const element = dialog.current;
        element?.showModal();
        return () => element?.close();
    }, []);
    return <dialog className="lifecycle-dialog" aria-label={title} ref={dialog}
        onCancel={(event) => { event.preventDefault(); close(); }}>
        <h1>{title}</h1>
        {children}
    </dialog>;
}

export function CreateVM({ create, close }: {
    create: (options: CreateOptions) => void;
    close: () => void;
}) {
    const [defaults, setDefaults] = useState<Defaults>();
    const [error, setError] = useState("");
    useEffect(() => {
        let active = true;
        Call.ByName("main.Lifecycle.Defaults").then(
            (value: Defaults) => { if (active) setDefaults(value); },
            (failure) => { if (active) setError(String(failure)); },
        );
        return () => { active = false; };
    }, []);

    function submit(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();
        const form = new FormData(event.currentTarget);
        const Name = String(form.get("name"));
        const size = (key: "CPUs" | "MemoryGiB" | "DiskGiB") => {
            const entered = Number(form.get(key));
            return entered === defaults?.[key] ? 0 : entered;
        };
        create({
            Name, Profile: String(form.get("profile")), CPUs: size("CPUs"),
            MemoryGiB: size("MemoryGiB"), DiskGiB: size("DiskGiB"),
        });
    }

    return <Dialog title="New VM" close={close}>
        {error && <p className="notice" role="alert">{error}</p>}
        <form className="lifecycle-form" onSubmit={submit}>
            <label>VM name<input id="new-vm-name" name="name" required autoFocus /></label>
            <p className="muted">Use 1–31 lowercase letters, digits or hyphens, starting with a letter.</p>
            {defaults ? <>
                <label>Profile<select name="profile" defaultValue={defaults.Profile}>
                    {defaults.Profiles.map((profile) => <option key={profile}>{profile}</option>)}
                </select></label>
                <div className="lifecycle-sizes">
                    {([["CPUs", "CPUs"], ["MemoryGiB", "Memory (GiB)"], ["DiskGiB", "Disk (GiB)"]] as const).map(([key, label]) =>
                        <label key={key}>{label}<input type="number" id={`new-vm-${key}`} name={key} min={1}
                            required defaultValue={defaults[key]} /></label>)}
                </div>
            </> : !error && <p role="status">Loading VM defaults…</p>}
            <div className="lifecycle-buttons">
                <button type="button" onClick={close}>Cancel</button>
                <button type="submit" className="primary" disabled={!defaults}>Create VM</button>
            </div>
        </form>
    </Dialog>;
}

export function VMActions({ name, busy, lifecycle }: {
    name: string; busy: boolean; lifecycle: LifecycleController;
}) {
    const [mode, setMode] = useState<"actions" | "delete">();
    const [confirmation, setConfirmation] = useState("");
    const close = () => { setMode(undefined); setConfirmation(""); };
    return <>
        <button type="button" disabled={busy} onClick={() => setMode("actions")}>More</button>
        {mode === "actions" && <Dialog key="actions" title={`Actions for ${name}`} close={close}>
            <div className="lifecycle-buttons">
                <button type="button" onClick={close}>Cancel</button>
                <button type="button" onClick={() => { close(); void lifecycle.run("Provision", name, name); }}>Re-run provisioning</button>
                <button type="button" className="lifecycle-danger" onClick={() => setMode("delete")}>Delete VM…</button>
            </div>
        </Dialog>}
        {mode === "delete" && <Dialog key="delete" title={`Delete ${name}?`} close={close}>
            <p>This permanently deletes the VM and its disks.</p>
            <form className="lifecycle-form" onSubmit={(event) => {
                event.preventDefault();
                if (confirmation !== name) return;
                close();
                void lifecycle.run("Delete", name, name, confirmation);
            }}>
                <label>Type {name} to confirm<input id="delete-vm-name" value={confirmation} autoFocus
                    onChange={(event) => setConfirmation(event.currentTarget.value)} /></label>
                <div className="lifecycle-buttons">
                    <button type="button" onClick={close}>Cancel</button>
                    <button className="lifecycle-danger" type="submit" disabled={confirmation !== name}>Delete VM</button>
                </div>
            </form>
        </Dialog>}
    </>;
}

const operationLabels = {
    Setup: ["Setting up this Mac…", "Host setup complete", "Host setup failed"],
    Create: ["Creating VM…", "VM created", "VM creation failed"],
    Provision: ["Provisioning VM…", "Provisioning complete", "Provisioning failed"],
    Delete: ["Deleting VM…", "VM deleted", "VM deletion failed"],
};

export function LifecycleStatus({ lifecycle }: { lifecycle: LifecycleController }) {
    const { host, job, run, dismiss } = lifecycle;
    const log = useRef<HTMLPreElement>(null);
    const lines = job?.progress?.lines;
    useEffect(() => {
        if (log.current) log.current.scrollTop = log.current.scrollHeight;
    }, [lines]);

    return <>
        {host?.error && <p className="notice header-notice" role="alert">{host.error}</p>}
        {host?.data === false && <div className="lifecycle-status">
            <p>Set up this Mac to create local VMs. Setup checks Lima and saves local-only port publishing.</p>
            <button type="button" disabled={job?.active} onClick={() => void run("Setup")}>Set up this Mac</button>
        </div>}
        {job && <section className="lifecycle-status" aria-label="VM operation progress">
            <div className="lifecycle-status-heading">
                <p id="lifecycle-result" role="status">{operationLabels[job.operation][job.active ? 0 : job.error ? 2 : 1]}
                    {job.name && ` · ${job.name}`}
                    {(job.active || job.error) && job.progress?.step && ` · ${job.progress.step}`}</p>
                {!job.active && <button type="button" onClick={dismiss}>Dismiss progress</button>}
            </div>
            {job.error && <p className="notice" role="alert">{job.error}</p>}
            {lines && lines.length > 0 && <pre id="lifecycle-log" aria-label="Operation log" tabIndex={0} ref={log}>{lines.join("\n")}</pre>}
            {!job.active && job.error && job.progress?.retry &&
                <button type="button" onClick={() => void run("Provision", job.name, job.name)}>Retry provisioning</button>}
        </section>}
    </>;
}
