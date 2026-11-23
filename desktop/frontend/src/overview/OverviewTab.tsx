import { useState } from "react";
import { Call } from "@wailsio/runtime";
import type { LocalStatus } from "../types";

const gib = 1024 ** 3;
const number = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
const footprint = new Intl.NumberFormat(undefined, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const clock = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", hourCycle: "h23" });

function StorageCard({ title, used, total, detail }: {
    title: string; used?: number; total?: number; detail?: string;
}) {
    return (
        <section className="card">
            <div className="muted">{title}</div>
            <div className="metric">{used === undefined ? "—" : number.format(used / gib)}
                {total !== undefined && <small> / {number.format(total / gib)} GiB</small>}
            </div>
            {used !== undefined && total !== undefined && total > 0 &&
                <progress aria-label={title} max={total} value={Math.min(total, used)} />}
            {detail && <div className="muted metric-detail">{detail}</div>}
        </section>
    );
}

export function OverviewTab({ status, showNetworkLogs }: { status: LocalStatus; showNetworkLogs: () => void }) {
    const [openError, setOpenError] = useState("");
    const usage = status.host ?? status.hostagent;
    const agents = status.agents ?? [];
    const ports = status.ports ?? [];
    const denials = (status.denials ?? []).slice(0, 5);
    const load = status.guest?.load?.[0];
    const memory = status.guest?.memory;
    const hostMemory = status.host?.memory_bytes;
    const agentMemory = status.hostagent?.memory_bytes;
    const memoryDetail = hostMemory !== undefined ? `${footprint.format(hostMemory / gib)} GiB on this Mac`
        : agentMemory !== undefined ? `${footprint.format(agentMemory / gib)} GiB on this Mac (Lima hostagent)` : undefined;

    async function openPort(port: number) {
        setOpenError("");
        try {
            await Call.ByName("main.Local.OpenPort", status.name, port);
        } catch (error) {
            setOpenError(String(error));
        }
    }

    return (
        <div className="content overview">
            {!!status.errors?.length && <p className="notice" role="status">Some readings are unavailable or stale. {status.errors.join(" · ")}</p>}
            <div className="metrics">
                <section className="card">
                    <div className="muted">{status.hostagent && !status.host ? "CPU (Lima hostagent)" : "CPU (seen from the Mac)"}</div>
                    <div className="metric">{usage ? `${number.format(usage.cpu_percent)}%` : "—"}</div>
                    <div className="muted">{status.cpus} cores{load !== undefined && ` · load ${load.toFixed(2)}`}</div>
                </section>
                <StorageCard title="Memory" used={memory?.used_bytes} total={memory?.total_bytes} detail={memoryDetail} />
                <StorageCard title="Disk" used={status.guest?.disk?.used_bytes} total={status.guest?.disk?.total_bytes} />
                <section className="card">
                    <div className="muted">Coding agents</div>
                    <div className="metric">{status.guest ? `${agents.length} running` : "—"}</div>
                    <div className="muted">{agents.map((agent) => agent.cwd ? `${agent.name} in ${agent.cwd}` : agent.name).join(" · ")}</div>
                </section>
            </div>
            <div className="details">
                <section className="card">
                    <h2>Servers</h2>
                    {openError && <p className="notice" role="alert">{openError}</p>}
                    {!ports.length && <p className="muted">{status.guest ? "No listening servers" : "Guest readings unavailable"}</p>}
                    {ports.map((port) => (
                        <div className="detail-row" key={`${port.address}:${port.guest_port}`}>
                            <code title={port.address}>:{port.guest_port}</code>
                            <span className="description">{port.processes?.map((process) => `${process.name} (pid ${process.pid})`).join(", ") || port.address}</span>
                            {port.published && port.host_url
                                ? <a href={port.host_url} onClick={(event) => { event.preventDefault(); void openPort(port.guest_port); }}>Open in browser</a>
                                : <span className="muted">not published</span>}
                        </div>
                    ))}
                </section>
                <section className="card">
                    <h2>Blocked network attempts</h2>
                    {!denials.length && <p className="muted">No recent blocked attempts</p>}
                    {denials.map((denial, index) => (
                        <div className="detail-row" key={`${denial.time}-${index}`}>
                            <code className="muted">{clock.format(new Date(denial.time))}</code>
                            <span className="description">{denial.destination
                                ? <>{denial.protocol?.toUpperCase()} to <code>{denial.destination}</code></> : denial.msg}</span>
                            <span className="warning">{denial.reason}</span>
                        </div>
                    ))}
                    <div className="detail-row">
                        <button className="text-button" onClick={showNetworkLogs}>See all in Logs</button>
                    </div>
                </section>
            </div>
        </div>
    );
}
