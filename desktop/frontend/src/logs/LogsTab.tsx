import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Call, Events } from "@wailsio/runtime";
import { appendLogs } from "./buffer";

const sources = [
    ["", "All"], ["guest", "Guest"], ["net", "Network"], ["host", "Host"], ["provision", "Provisioning"],
] as const;
export type LogSource = typeof sources[number][0];

interface LogLine {
    time: string;
    source: string;
    level?: string;
    unit?: string;
    identifier?: string;
    text: string;
}

const clock = new Intl.DateTimeFormat(undefined, {
    hour: "2-digit", minute: "2-digit", second: "2-digit", fractionalSecondDigits: 3, hourCycle: "h23",
});
let nextStream = 0;

export function LogsTab({ name, initialSource }: { name: string; initialSource: LogSource }) {
    const [source, setSource] = useState(initialSource);
    const [input, setInput] = useState("");
    const [unit, setUnit] = useState("");
    const [follow, setFollow] = useState(true);
    const [lines, setLines] = useState<(LogLine & { key: number })[]>([]);
    const [error, setError] = useState("");
    const [pending, setPending] = useState(true);
    const pane = useRef<HTMLDivElement>(null);
    const atEnd = useRef(true);
    const canFilterUnit = source === "" || source === "guest";
    const activeUnit = canFilterUnit ? unit : "";

    useEffect(() => {
        setLines([]);
        setPending(true);
        setError("");
        atEnd.current = true;
        let active = true;
        let nextLine = 0;
        const id = String(++nextStream);
        const unsubscribe = Events.On("local:logs", ({ data }) => {
            const batch: { id: string; lines: LogLine[] } = data;
            if (batch.id !== id) return;
            const incoming = batch.lines.map((line) => ({ ...line, key: ++nextLine }));
            setLines((previous) => appendLogs(previous, incoming));
            setPending(false);
        });
        const stream = Call.ByName("main.Local.StreamLogs", id, name, source, activeUnit, follow);
        void stream.then(
            () => { if (active) setPending(false); },
            (failure) => {
                if (!active) return;
                setError(String(failure));
                setPending(false);
            },
        );
        return () => {
            active = false;
            unsubscribe();
            void stream.cancel();
        };
    }, [name, source, activeUnit, follow]);

    useLayoutEffect(() => {
        const element = pane.current;
        if (element && atEnd.current) element.scrollTop = element.scrollHeight;
    }, [lines]);

    return (
        <div className="logs">
            <div className="log-toolbar">
                {sources.map(([value, label]) => <button className="chip" key={value}
                    aria-pressed={source === value} onClick={() => setSource(value)}>{label}</button>)}
                <span className="spacer" />
                <form onSubmit={(event) => { event.preventDefault(); setUnit(input.trim()); }}>
                    <label>Unit <input value={input} onChange={(event) => setInput(event.target.value)}
                        maxLength={128} disabled={!canFilterUnit} placeholder="e.g. vite.service" /></label>
                    <button disabled={!canFilterUnit}>Apply</button>
                </form>
                <label><input type="checkbox" checked={follow} onChange={(event) => setFollow(event.target.checked)} /> Follow</label>
            </div>
            {error && <p className="notice log-notice" role="alert">{error}</p>}
            <div className="log-pane" ref={pane} aria-label="Log output" tabIndex={0} onScroll={(event) => {
                const element = event.currentTarget;
                atEnd.current = element.scrollHeight - element.scrollTop - element.clientHeight < 16;
            }}>
                {!lines.length && <p className="muted">{pending ? (follow ? "Waiting for log entries…" : "Loading logs…") : "No log entries"}</p>}
                {lines.map((line) => (
                    <div className="log-line" key={line.key}>
                        <span className="log-time">{clock.format(new Date(line.time))}</span>
                        <span className={`log-source ${line.source === "net" ? "warning" : ""}`}>{line.source}</span>
                        <span className={line.level === "warn" ? "warning" : "muted"}>{line.level}</span>
                        <span className="log-message">
                            {(line.unit || line.identifier) && <span className="muted">{line.unit || line.identifier}: </span>}
                            {line.text}
                        </span>
                    </div>
                ))}
            </div>
        </div>
    );
}
