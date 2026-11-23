import { useState } from "react";
import { Call } from "@wailsio/runtime";
import { FilesTab } from "./files/FilesTab";
import { LogsTab, type LogSource } from "./logs/LogsTab";
import { OverviewTab } from "./overview/OverviewTab";
import { statePill } from "./pill";
import { AddRemoteHost, RemoteView } from "./remote/RemoteView";
import { useRemoteHosts } from "./remote/useRemoteHosts";
import { TerminalTab } from "./terminal/TerminalTab";
import type { DoctorResult, LocalStatus, VMRef, VMStatus } from "./types";
import { usePolling } from "./usePolling";
import "./styles.css";

const tabs = ["Overview", "Logs", "Terminal", "Files"] as const;
type Tab = typeof tabs[number];
type Action = "Start" | "Stop" | "Doctor";
const progress: Record<Action, string> = { Start: "Starting VM…", Stop: "Stopping VM…", Doctor: "Running isolation checks…" };

const listLocal = (): Promise<VMStatus[]> => Call.ByName("main.Local.ListStatus");
const readLocal = (name: string): Promise<LocalStatus> => Call.ByName("main.Local.Status", name);

function retainGuestReadings(previous: LocalStatus | undefined, next: LocalStatus): LocalStatus {
    if (next.state !== "running" || next.guest || !next.errors?.length || !previous?.guest) return next;
    return { ...next, guest: previous.guest, agents: previous.agents, ports: previous.ports };
}

function uptime(seconds: number) {
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor(seconds / 60) % 60;
    return hours ? `${hours}h ${minutes}m` : minutes ? `${minutes}m` : "<1m";
}

function Dot({ tone }: { tone: string }) {
    return <span className={`dot ${tone}`} aria-hidden="true" />;
}

function DoctorPill({ result }: { result?: DoctorResult }) {
    if (!result) return <span className="pill idle">Doctor has not run</span>;
    const minutes = Math.max(0, Math.floor((Date.now() - Date.parse(result.checked_at)) / 60_000));
    const title = result.failed ? (result.failing_checks ?? []).join(", ") : `${result.passed} checks passed, ${result.skipped} skipped`;
    return <span className={`pill ${result.failed ? "bad" : "doctor"}`} title={title}>
        {result.failed ? `${result.failed} isolation checks failed` : "Isolation checks passed"}
        {" · "}{minutes ? `${uptime(minutes * 60)} ago` : "just now"}
    </span>;
}

function VMRow({ id, name, host, state, selected, select }: {
    id?: string; name: string; host?: string; state: string; selected: boolean; select: () => void;
}) {
    const pill = statePill(state);
    return <button type="button" id={id} className="vm-row" aria-current={selected} onClick={select}>
        <Dot tone={pill.tone} /><span className="vm-name" title={name}>{name}
            {host && <span className="muted"> · {host}</span>}</span>
        <span className="muted row-state">{pill.label.toLowerCase()}</span>
    </button>;
}

export default function App() {
    const remoteHosts = useRemoteHosts();
    const [selection, setSelection] = useState<VMRef>();
    const [tab, setTab] = useState<Tab>("Overview");
    const [logSource, setLogSource] = useState<LogSource>("");
    const [revision, setRevision] = useState(0);
    const [pending, setPending] = useState<Action>();
    const [actionError, setActionError] = useState("");
    const inventory = usePolling("local", listLocal, revision);
    const localVMs = inventory?.data ?? [];
    const selectedLocal = localVMs.find((vm) => selection?.kind === "local" && selection.name === vm.name);
    const remote = remoteHosts.vms.find((vm) => selection?.kind === "remote" && selection.alias === vm.id);
    const local = remote ? undefined : selectedLocal ?? localVMs[0];
    const vm: VMRef | undefined = local ? { kind: "local", name: local.name } : remote ? { kind: "remote", alias: remote.id } : undefined;
    const detail = usePolling(tab === "Overview" ? local?.name : undefined, readLocal, revision, retainGuestReadings);
    const current = remote ? remote.check?.status : detail?.data ?? local;
    const pill = statePill(current?.state ?? "");
    const running = local?.state === "running";
    const transitioning = local?.state === "provisioning";

    function select(next: VMRef) {
        setSelection(next);
        setActionError("");
    }

    function showLogs(source: LogSource) {
        setLogSource(source);
        setTab("Logs");
    }

    async function run(action: Action) {
        if (!local || pending) return;
        setPending(action);
        setActionError("");
        try {
            await Call.ByName(`main.Local.${action}`, local.name);
        } catch (error) {
            setActionError(`${local.name}: ${String(error)}`);
        } finally {
            setRevision((value) => value + 1);
            setPending(undefined);
        }
    }

    return (
        <>
            <div className="title-strip">homebox</div>
            <div className="workspace">
                <nav className="sidebar" aria-label="Virtual machines">
                    <div className="vm-list">
                        <h2>This Mac</h2>
                        {localVMs.map((entry) => <VMRow key={entry.name} name={entry.name} state={entry.state}
                            selected={local?.name === entry.name} select={() => select({ kind: "local", name: entry.name })} />)}
                        {inventory && !inventory.error && localVMs.length === 0 &&
                            <p className="sidebar-hint">No VMs here. Create one with <code>box new NAME</code>.</p>}
                        {inventory?.error && <p className="notice" role="status">{inventory.error}</p>}
                        <h2>Other hosts</h2>
                        {remoteHosts.vms.map((entry) => <VMRow key={entry.id} id={`remote-row-${entry.id}`} name={entry.name}
                            host={entry.host} state={entry.check?.state ?? (entry.error ? "Check failed" : "Checking")}
                            selected={remote?.id === entry.id} select={() => select({ kind: "remote", alias: entry.id })} />)}
                        {remoteHosts.error && <p className="notice" role="alert">{remoteHosts.error}</p>}
                    </div>
                    <AddRemoteHost add={remoteHosts.add} />
                </nav>
                <main>
                    {remote && remote.check?.state !== "reachable"
                        ? <RemoteView key={remote.id} vm={remote} checkAgain={() => void remoteHosts.check(remote.id)} />
                        : vm && current ? <>
                        <header className="vm-header" role="group" aria-label="VM status and actions" id="vm-header">
                            <h1>{current.name}</h1>
                            <span className={`pill ${pill.tone}`}>
                                <Dot tone={pill.tone} />{pill.label}
                                {current.uptime_seconds !== undefined && (running || remote) && ` · up ${uptime(current.uptime_seconds)}`}
                            </span>
                            {local && <DoctorPill result={local.doctor} />}
                            <span className="spacer" />
                            {local && <>
                                <button disabled={!running || !!pending} onClick={() => void run("Doctor")}>Run doctor</button>
                                <button disabled={transitioning || !!pending} onClick={() => void run(running ? "Stop" : "Start")}>{running ? "Stop" : "Start"}</button>
                            </>}
                            {remote && <button disabled={remote.checking} onClick={() => void remoteHosts.check(remote.id)}>Check again</button>}
                            <button className="primary" onClick={() => setTab("Terminal")}>Open terminal</button>
                        </header>
                        {local && pending && <p className="notice header-notice" role="status">{progress[pending]}</p>}
                        {actionError && <p className="notice header-notice" role="alert">{actionError}</p>}
                        {local?.provisioning?.step && <p className="notice header-notice" role="status">
                            {local.provisioning.step}{local.provisioning.error && ` · ${local.provisioning.error}`}
                        </p>}
                        <div className="tabs" id="vm-tabs" role="tablist" aria-label="VM views">
                            {tabs.map((item) => <button key={item} id={`tab-${item}`} aria-controls="vm-view"
                                role="tab" aria-selected={tab === item} onClick={() => item === "Logs" ? showLogs("") : setTab(item)}>{item}</button>)}
                        </div>
                        <div className="view" id="vm-view" role="tabpanel" aria-labelledby={`tab-${tab}`}>
                            {tab === "Overview" && (local ? <>
                                {detail?.error && <p className="notice header-notice" role="status">
                                    {detail.data ? "Showing stale status. " : ""}{detail.error}
                                </p>}
                                {detail?.data && <OverviewTab key={local.name} kind="local" status={detail.data} showNetworkLogs={() => showLogs("net")} />}
                                {!detail?.data && !detail?.error && <p className="content muted" role="status">Loading VM status…</p>}
                            </> : <OverviewTab key={remote?.id} kind="remote" status={current} />)}
                            {tab === "Logs" && (local ? <LogsTab key={`${local.name}:${logSource}`} name={local.name} initialSource={logSource} />
                                : <p className="content">View guest logs for this VM from its host Mac.</p>)}
                            {tab === "Terminal" && <TerminalTab key={local?.name ?? remote?.id} vm={vm} />}
                            {tab === "Files" && <FilesTab key={local?.name ?? remote?.id} vm={vm} />}
                        </div>
                    </> : <div className="content empty">
                        {inventory?.error ? <><h1>Local VMs unavailable</h1><p role="alert">{inventory.error}</p></>
                            : inventory?.data ? <><h1>No VMs on this Mac</h1><p>Create one in your terminal with <code>box new NAME</code>.</p></>
                                : <p role="status">Loading virtual machines…</p>}
                    </div>}
                </main>
            </div>
        </>
    );
}
