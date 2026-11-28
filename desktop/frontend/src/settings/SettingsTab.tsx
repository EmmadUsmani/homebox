import { thisHost } from "../platform";
import { useEffect, useState } from "react";
import { Call, Clipboard, Dialogs } from "@wailsio/runtime";
import "./settings.css";

const call = <T,>(method: string, ...args: unknown[]) =>
    Call.ByName("main.Settings." + method, ...args) as Promise<T>;

type Port = { GuestPort: number; HostPort: number; HostIP: string };
type Size = { CPUs: number; MemoryGiB: number; DiskGiB: number };
type Fingerprints = { saved: string[]; detected: string[] };
const emptySize: Size = { CPUs: 0, MemoryGiB: 0, DiskGiB: 0 };
const sizeFields = [
    ["CPUs", "CPUs"], ["MemoryGiB", "Memory (GiB)"], ["DiskGiB", "Disk (GiB)"],
] as const;

async function confirm(title: string, message: string, label: string) {
    return await Dialogs.Question({
        Title: title, Message: message,
        Buttons: [{ Label: "Cancel", IsCancel: true, IsDefault: true }, { Label: label }],
    }) === label;
}

function useSettingsAction() {
    const [pending, setPending] = useState(false);
    const [message, setMessage] = useState("");
    const [error, setError] = useState("");
    async function run(action: () => Promise<void>) {
        setPending(true);
        setError("");
        setMessage("");
        try {
            await action();
        } catch (cause) {
            setError(String(cause));
        } finally {
            setPending(false);
        }
    }
    return {
        pending, run, setMessage,
        feedback: error ? <p className="notice" role="alert">{error}</p> : message && <p role="status">{message}</p>,
    };
}

function PublishedPorts({ name, stopped, running }: { name: string; stopped: boolean; running: boolean }) {
    const [ports, setPorts] = useState<Port[]>();
    const [guest, setGuest] = useState("");
    const [host, setHost] = useState("");
    const [replacement, setReplacement] = useState<string>();
    const { pending, run, feedback, setMessage } = useSettingsAction();

    useEffect(() => {
        void run(async () => setPorts(await call<Port[] | null>("Ports", name) ?? []));
    }, [name]);

    async function update(options: { Add?: string[]; Remove?: string[]; Set?: string[] }) {
        setPorts(await call<Port[] | null>("UpdatePorts", name, options) ?? []);
        setMessage("Published ports updated. Start the VM to apply them.");
    }

    return <section className="card settings-card">
        <h2>Published ports</h2>
        <p className="muted">Forward guest TCP ports to {thisHost}. The listening address determines who can connect.</p>
        {!stopped && <p className="muted">Stop the VM before changing its published ports.</p>}
        <form className="settings-form" onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
                if (await confirm("Publish port?", `Forward guest port ${guest} to port ${host} on ${thisHost} using the host’s publishing settings?`, "Publish")) {
                    await update({ Add: [`${guest}:${host}`] });
                    setGuest("");
                    setHost("");
                }
            });
        }}>
            <label>Guest port <input inputMode="numeric" required value={guest} onChange={(event) => setGuest(event.target.value)} disabled={!stopped || pending} /></label>
            <label>Host port <input inputMode="numeric" required value={host} onChange={(event) => setHost(event.target.value)} disabled={!stopped || pending} /></label>
            <button disabled={!stopped || pending || !ports}>Publish port</button>
            <button type="button" disabled={!stopped || pending || !ports} onClick={() => setReplacement(
                ports?.map(({ GuestPort, HostPort }) => `${GuestPort}:${HostPort}`).join("\n") ?? "",
            )}>Replace mappings…</button>
        </form>
        {replacement !== undefined && <form className="settings-form" onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
                if (await confirm("Replace published ports?", "This replaces all current forwards. An empty list removes every forward.", "Replace ports")) {
                    await update({ Set: replacement.split(/[\n,]/).map((line) => line.trim()).filter(Boolean) });
                    setReplacement(undefined);
                }
            });
        }}>
            <label>Guest:host mappings, one per line
                <textarea value={replacement} onChange={(event) => setReplacement(event.target.value)} disabled={!stopped || pending} />
            </label>
            <button disabled={!stopped || pending}>Review replacement</button>
            <button type="button" disabled={pending} onClick={() => setReplacement(undefined)}>Cancel replacement</button>
        </form>}
        {!!ports?.length && <div className="settings-port-list" role="region" aria-label="Published port mappings" tabIndex={0}>
            {ports.map((port) => <div className="detail-row settings-port" key={port.GuestPort}>
                <code className="description">Guest {port.GuestPort} → {port.HostIP}:{port.HostPort}</code>
                <button disabled={pending || !running} aria-label={`Open port ${port.GuestPort} in browser`}
                    onClick={() => void run(() => Call.ByName("main.Local.OpenPort", name, port.GuestPort))}>Open in browser</button>
                <button disabled={pending} aria-label={`Copy URL for port ${port.GuestPort}`} onClick={() => void run(async () => {
                    await Clipboard.SetText(await call<string>("PortURL", name, port.GuestPort));
                    setMessage("URL copied.");
                })}>Copy URL</button>
                <button disabled={pending || !stopped} aria-label={`Unpublish port ${port.GuestPort}`}
                    onClick={() => void run(async () => {
                        if (await confirm("Unpublish port?", `Remove the forward for guest port ${port.GuestPort}?`, "Unpublish")) {
                            await update({ Remove: [String(port.GuestPort)] });
                        }
                    })}>Unpublish</button>
            </div>)}
        </div>}
        {ports?.length === 0 && <p className="muted">No published ports.</p>}
        {feedback}
    </section>;
}

function VMSize({ name, stopped }: { name: string; stopped: boolean }) {
    const [configured, setConfigured] = useState<Size>();
    const [changes, setChanges] = useState<Partial<Record<keyof Size, string>>>({});
    const { pending, run, feedback, setMessage } = useSettingsAction();

    useEffect(() => {
        void run(async () => setConfigured(await call<Size>("Resize", name, emptySize)));
    }, [name]);

    return <section className="card settings-card">
        <h2>VM size</h2>
        <p className="muted">{stopped ? "The disk can only grow." : "Stop the VM before resizing. The disk can only grow."}</p>
        {configured && <form className="settings-form" onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
                const size = { ...emptySize };
                for (const [field] of sizeFields) {
                    const value = changes[field];
                    if (value !== undefined && Number(value) !== configured[field]) size[field] = Number(value);
                }
                if (size.DiskGiB > configured.DiskGiB && !await confirm("Grow VM disk?",
                    `Grow the disk from ${configured.DiskGiB} to ${size.DiskGiB} GiB? The disk cannot be shrunk later.`, "Grow disk")) return;
                setConfigured(await call<Size>("Resize", name, size));
                setChanges({});
                setMessage("VM size saved.");
            });
        }}>
            {sizeFields.map(([field, label]) => <label key={field}>{label}
                <input type="number" step="1" min="1" required disabled={!stopped || pending}
                    value={changes[field] ?? configured[field]} onChange={(event) => setChanges({ ...changes, [field]: event.target.value })} />
            </label>)}
            <button disabled={!stopped || pending || !Object.keys(changes).length}>Save VM size</button>
        </form>}
        {feedback}
    </section>;
}

function SSHHostKeys({ name, running }: { name: string; running: boolean }) {
    const [review, setReview] = useState<Fingerprints>();
    const { pending, run, feedback, setMessage } = useSettingsAction();

    return <section className="card settings-card" id="ssh-host-keys" tabIndex={-1}>
        <h2>SSH host keys</h2>
        <p className="muted">Replace the saved keys only if you intentionally changed the guest’s SSH identity. An unexpected key can mean another machine is answering.</p>
        {!running && <p className="muted">Start the VM to read its SSH host keys.</p>}
        <button disabled={!running || pending} onClick={() => void run(async () => {
            setReview(undefined);
            setReview(await call<Fingerprints>("ReviewSSHKeys", name));
        })}>Review SSH host keys</button>
        {review && <div className="settings-fingerprints">
            <div><h3>Saved fingerprints</h3><pre>{review.saved.join("\n") || "None"}</pre></div>
            <div><h3>Detected fingerprints</h3><pre>{review.detected.join("\n") || "None"}</pre></div>
            <button disabled={!running || pending} onClick={() => void run(async () => {
                if (!await confirm("Replace saved SSH host keys?", "Confirm that you intentionally changed the guest’s SSH keys and verified the detected fingerprints.", "Replace SSH keys")) return;
                await call("ReplaceSSHKeys", name, review);
                setReview(undefined);
                setMessage("SSH host keys updated. Regenerate and reinstall the laptop SSH configuration and pins.");
            })}>Replace saved SSH keys…</button>
        </div>}
        {feedback}
    </section>;
}

const sshOutputs = [
    ["Config", "SSH config"], ["KnownHosts", "Pinned known hosts"], ["Installer", "Laptop installer"], ["Include", "Local Include line"],
] as const;
type SSHOutput = typeof sshOutputs[number][0];

function LaptopSSHConfig() {
    const [host, setHost] = useState("");
    const [mode, setMode] = useState<SSHOutput>("Config");
    const [output, setOutput] = useState<string>();
    const { pending, run, feedback, setMessage } = useSettingsAction();
    const usesHost = mode === "Config" || mode === "Installer";

    return <section className="card settings-card">
        <h2>Laptop SSH config</h2>
        <p className="muted">Export SSH aliases for the VMs on {thisHost}. The laptop installer includes the pinned keys.</p>
        <form className="settings-form" onSubmit={(event) => {
            event.preventDefault();
            void run(async () => {
                setOutput(undefined);
                setOutput(await call<string>("SSHConfig", {
                    Host: usesHost ? host.trim() : "", KnownHosts: mode === "KnownHosts", Installer: mode === "Installer", Include: mode === "Include",
                }));
            });
        }}>
            <label>Output <select value={mode} disabled={pending} onChange={(event) => {
                setMode(event.target.value as SSHOutput);
                setOutput(undefined);
            }}>
                {sshOutputs.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </select></label>
            <label>Host address <input placeholder={`Use ${thisHost}’s local name`} value={host} disabled={!usesHost || pending}
                onChange={(event) => { setHost(event.target.value); setOutput(undefined); }} /></label>
            <button disabled={pending}>Show SSH config</button>
        </form>
        {output !== undefined && <>
            <pre aria-label="SSH config output" tabIndex={0}>{output || "No SSH configuration to display."}</pre>
            <button disabled={!output || pending} onClick={() => void run(async () => {
                await Clipboard.SetText(output);
                setMessage("SSH config copied.");
            })}>Copy SSH config</button>
        </>}
        {feedback}
    </section>;
}

export function SettingsTab({ name, state, focusSSHKeys }: { name: string; state: string; focusSSHKeys: boolean }) {
    const stopped = state === "stopped";
    const running = state === "running";
    useEffect(() => {
        if (focusSSHKeys) document.getElementById("ssh-host-keys")?.focus();
    }, [focusSSHKeys]);
    return <div className="content settings">
        <PublishedPorts name={name} stopped={stopped} running={running} />
        <VMSize name={name} stopped={stopped} />
        <SSHHostKeys name={name} running={running} />
        <LaptopSSHConfig />
    </div>;
}
