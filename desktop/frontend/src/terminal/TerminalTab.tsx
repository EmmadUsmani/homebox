import { isMac } from "../platform";
import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { Call, Clipboard } from "@wailsio/runtime";
import "@xterm/xterm/css/xterm.css";
import type { VMRef } from "../types";
import { inputChunkEnd } from "./input";
import "./terminal.css";

const service = "github.com/EmmadUsmani/homebox/desktop/session.Terminal.";
const call = <T,>(method: string, ...args: unknown[]) =>
    Call.ByName(service + method, ...args) as Promise<T>;

type OpenResult = { id?: string; error?: string; keyChanged?: boolean };
type Output = { data?: string; ended?: boolean };
type Status = { kind: "opening" | "connected" | "ended" } |
    { kind: "error" | "keyChanged"; message: string };

type ConnectionProps = { vm: VMRef; openSSHSettings?: () => void };

export function HostKeyWarning({ vm, openSSHSettings }: ConnectionProps) {
    return (
        <div className="terminal-status terminal-host-key" role="alert">
            <strong>Host key changed</strong>
            <p>The VM answered with a different key from the one pinned when it was set up.
                It may have been rebuilt, or another machine may be answering in its place.
                The connection is blocked until you verify the VM.</p>
            {openSSHSettings
                ? <button type="button" onClick={openSSHSettings}>Open SSH host key settings</button>
                : <p>If you rebuilt it on purpose, review and replace the SSH keys in Settings on the {isMac ? "Mac" : "host"} hosting the VM, or run <code>{`box repin ${vm.kind === "remote" ? vm.alias : vm.name}`}</code> there. Then reinstall the laptop SSH config.</p>}
        </div>
    );
}

function TerminalPane({ vm, selected, openSSHSettings }: ConnectionProps & { selected: boolean }) {
    const host = useRef<HTMLDivElement>(null);
    const mirror = useRef<HTMLPreElement>(null);
    const fitAndFocus = useRef<() => void>(() => {});
    const [status, setStatus] = useState<Status>({ kind: "opening" });
    const [attempt, setAttempt] = useState(0);

    useEffect(() => {
        const element = host.current!;
        const accessibleOutput = mirror.current!;
        const terminal = new Terminal({
            fontFamily: '"JetBrains Mono", monospace',
            fontSize: 13,
            lineHeight: 1.2,
            cursorBlink: true,
            scrollback: 1000,
            theme: {
                background: "#0b0d11", foreground: "#e6e8ee", cursor: "#e6e8ee",
                selectionBackground: "#304366", green: "#7ad8b0", blue: "#7fb0ff",
            },
        });
        terminal.parser.registerOscHandler(8, () => true);
        terminal.parser.registerOscHandler(52, () => true);
        const fit = new FitAddon();
        terminal.loadAddon(fit);
        terminal.open(element);
        const fitVisible = () => {
            if (element.clientWidth > 0 && element.clientHeight > 0) fit.fit();
        };
        fitAndFocus.current = () => {
            fitVisible();
            terminal.focus();
        };
        const observer = new ResizeObserver(fitVisible);
        observer.observe(element);
        fitVisible();

        let stopped = false;
        let id: string | undefined;
        let resizePending = false;
        let latestSize: { rows: number; cols: number } | undefined;
        const sendSize = async () => {
            if (resizePending || !id) return;
            resizePending = true;
            try {
                while (latestSize && !stopped && id) {
                    const { rows, cols } = latestSize;
                    latestSize = undefined;
                    await call("Resize", id, rows, cols);
                }
            } catch {
                // A pending read reports transport closure.
            } finally {
                resizePending = false;
            }
        };
        terminal.onResize((size) => {
            latestSize = size;
            void sendSize();
        });

        let pendingInput = "";
        let writing = false;
        async function sendInput() {
            if (writing || !id) return;
            writing = true;
            try {
                while (pendingInput && !stopped && id) {
                    const end = inputChunkEnd(pendingInput);
                    const chunk = pendingInput.slice(0, end);
                    pendingInput = pendingInput.slice(end);
                    await call("Write", id, chunk);
                }
            } catch {
                // A pending read reports transport closure.
            } finally {
                writing = false;
            }
        }
        terminal.onData((data) => {
            if (!id) return;
            if (pendingInput.length + data.length > 256 * 1024) {
                terminal.write("\r\n[Input exceeds the available buffer]\r\n");
                return;
            }
            pendingInput += data;
            void sendInput();
        });
        terminal.attachCustomKeyEventHandler((event) => {
            const clipboardShortcut = isMac ? event.metaKey && !event.ctrlKey && !event.shiftKey
                : event.ctrlKey && event.shiftKey && !event.metaKey;
            if (!clipboardShortcut || event.altKey) return true;
            const key = event.key.toLowerCase();
            if (key !== "c" && key !== "v") return true;
            event.preventDefault();
            if (event.type !== "keydown" || event.repeat) return false;
            if (key === "c" && terminal.hasSelection()) {
                void Clipboard.SetText(terminal.getSelection()).catch(console.error);
            }
            if (key === "v") {
                void Clipboard.Text().then((text) => {
                    if (!stopped) terminal.paste(text);
                }).catch(console.error);
            }
            return false;
        });

        // Expose a bounded snapshot for assistive technology without announcing
        // every character or mirroring escape sequences from the SSH stream.
        const snapshot = window.setInterval(() => {
            const buffer = terminal.buffer.active;
            const start = Math.max(0, buffer.baseY + buffer.cursorY - 11);
            const lines: string[] = [];
            for (let index = start; index <= buffer.baseY + buffer.cursorY; index++) {
                lines.push(buffer.getLine(index)?.translateToString(true) ?? "");
            }
            const text = lines.join("\n");
            if (accessibleOutput.textContent !== text) accessibleOutput.textContent = text;
        }, 250);

        const close = (session: string) => call("Close", session).catch(console.error);
        async function connect() {
            try {
                const result = await call<OpenResult>("Open", vm, terminal.rows, terminal.cols);
                if (stopped) {
                    if (result.id) void close(result.id);
                    return;
                }
                if (!result.id) {
                    setStatus({
                        kind: result.keyChanged ? "keyChanged" : "error",
                        message: result.error ?? "SSH connection failed",
                    });
                    return;
                }
                id = result.id;
                setStatus({ kind: "connected" });
                void sendSize();
                while (!stopped) {
                    const output = await call<Output>("Read", id);
                    if (stopped || output.ended) break;
                    if (output.data) {
                        const bytes = Uint8Array.from(atob(output.data), (char) => char.charCodeAt(0));
                        await new Promise<void>((resolve) => terminal.write(bytes, resolve));
                    }
                }
                if (!stopped) setStatus({ kind: "ended" });
            } catch (error) {
                if (!stopped) setStatus(id ? { kind: "ended" } : { kind: "error", message: String(error) });
            } finally {
                if (id) void close(id);
                id = undefined;
            }
        }
        setStatus({ kind: "opening" });
        void document.fonts.load('13px "JetBrains Mono"').then(() => {
            if (!stopped) fitVisible();
        }).catch(console.error);
        void connect();
        return () => {
            stopped = true;
            if (id) void close(id);
            observer.disconnect();
            window.clearInterval(snapshot);
            terminal.dispose();
            accessibleOutput.textContent = "";
            fitAndFocus.current = () => {};
        };
    }, [vm, attempt]);

    useEffect(() => {
        if (selected && status.kind === "connected") fitAndFocus.current();
    }, [selected, status.kind]);

    return (
        <div className="terminal-pane" hidden={!selected}>
            <div className="terminal-viewport" ref={host}
                data-obscured={status.kind !== "connected" && status.kind !== "ended"} />
            <pre ref={mirror} className="terminal-accessible" aria-label="Recent terminal output" />
            {status.kind === "connected" && (
                <span className="terminal-accessible">Terminal connected</span>
            )}
            {status.kind === "keyChanged" && <HostKeyWarning vm={vm} openSSHSettings={openSSHSettings} />}
            {status.kind !== "connected" && status.kind !== "keyChanged" && (
                <div className={`terminal-status terminal-status-${status.kind}`} role="status">
                    {status.kind === "opening" && "Connecting…"}
                    {status.kind === "error" && status.message}
                    {status.kind === "ended" && "Session ended — "}
                    {(status.kind === "ended" || status.kind === "error") && (
                        <button type="button" onClick={() => setAttempt(attempt + 1)}>Reconnect</button>
                    )}
                </div>
            )}
        </div>
    );
}

function Sessions({ vm: reference, openSSHSettings }: ConnectionProps) {
    const [vm] = useState(reference);
    const [tabs, setTabs] = useState([1]);
    const [selected, setSelected] = useState(1);
    const next = useRef(2);
    const label = vm.kind === "local" ? vm.name : vm.alias;

    function add() {
        const id = next.current++;
        setTabs([...tabs, id]);
        setSelected(id);
    }

    function close(id: number) {
        const remaining = tabs.filter((tab) => tab !== id);
        setTabs(remaining);
        if (id === selected) setSelected(remaining[Math.min(tabs.indexOf(id), remaining.length - 1)] ?? 0);
    }

    return (
        <section className="terminal-tab" aria-label="Terminal">
            <div className="terminal-tabs" aria-label="Terminal sessions">
                {tabs.map((id) => (
                    <div key={id} className="terminal-tab-item" data-selected={selected === id}>
                        <button type="button" aria-pressed={selected === id} onClick={() => setSelected(id)}>{label} · {id}</button>
                        <button type="button" aria-label={`Close session ${id}`} onClick={() => close(id)}>×</button>
                    </div>
                ))}
                <button type="button" className="terminal-add" aria-label="New terminal session" disabled={tabs.length >= 12} onClick={add}>+</button>
            </div>
            <div className="terminal-body">
                {tabs.map((id) => <TerminalPane key={id} vm={vm} selected={id === selected} openSSHSettings={openSSHSettings} />)}
            </div>
            <footer className="terminal-footer">
                <span>SSH with the pinned host key · no agent or X11 forwarding</span>
                <span>{isMac ? "Copy: ⌘C · Paste: ⌘V" : "Copy: Ctrl+Shift+C · Paste: Ctrl+Shift+V"}</span>
            </footer>
        </section>
    );
}

export function TerminalTab({ vm, openSSHSettings }: ConnectionProps) {
    // A caller can recreate its VMRef on each render. Keep channel lifetimes
    // tied to the VM identity rather than the identity of the props object.
    const key = vm.kind === "local" ? vm.name : vm.alias;
    return <Sessions key={`${vm.kind}:${key}`} vm={vm} openSSHSettings={openSSHSettings} />;
}
