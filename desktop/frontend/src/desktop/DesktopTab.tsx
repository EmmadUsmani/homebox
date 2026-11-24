import { useEffect, useRef, useState } from "react";
import { Call, Events } from "@wailsio/runtime";
import { HostKeyWarning } from "../terminal/TerminalTab";
import type { VMRef } from "../types";
import "./desktop.css";

type Result = {
    state: "idle" | "connecting" | "connected" | "installing" | "missing" | "disconnected" | "keyChanged" | "error";
    message?: string;
};

type Pending = ReturnType<typeof Call.ByName>;
const call = (method: string, ...args: unknown[]) => Call.ByName("main.Desktop." + method, ...args);

let nextAttempt = 0;

function bounds(element: HTMLElement) {
    const { x, y, width, height } = element.getBoundingClientRect();
    return { X: x, Y: y, Width: width, Height: height };
}

export function DesktopTab({ vm }: { vm: VMRef }) {
    const viewport = useRef<HTMLDivElement>(null);
    const pending = useRef<Pending>();
    const [status, setStatus] = useState<Result>({ state: "idle" });
    const name = vm.kind === "local" ? vm.name : vm.alias;

    useEffect(() => () => {
        const request = pending.current;
        pending.current = undefined;
        if (request) void request.cancel();
    }, []);

    async function run(method: "Connect" | "Install") {
        if (pending.current || !viewport.current) return;
        const element = viewport.current;
        const id = String(++nextAttempt);
        const resize = () => void call("Resize", id, bounds(element)).catch(console.error);
        const observer = new ResizeObserver(resize);
        const unsubscribe = Events.On("desktop:connected", ({ data }) => {
            if (data !== id || pending.current !== request) return;
            setStatus({ state: "connected" });
            resize();
        });
        observer.observe(element);
        window.addEventListener("resize", resize);
        setStatus({ state: method === "Connect" ? "connecting" : "installing" });
        const request = method === "Connect"
            ? call(method, vm, id, bounds(element))
            : call(method, vm);
        pending.current = request;
        try {
            const result = await request as Result;
            if (pending.current === request) setStatus(result);
        } catch (error) {
            if (pending.current === request) setStatus({ state: "error", message: String(error) });
        } finally {
            if (pending.current === request) pending.current = undefined;
            unsubscribe();
            observer.disconnect();
            window.removeEventListener("resize", resize);
        }
    }

    return <div ref={viewport} className="terminal-body desktop-pane" aria-label="Remote desktop">
        {status.state === "keyChanged" && <HostKeyWarning vm={vm} />}
        {status.state !== "connected" && status.state !== "keyChanged" &&
            <div className="terminal-status">
                <p role={status.state === "error" ? "alert" : "status"}>
                    {status.state === "idle" && "Desktop is disconnected."}
                    {status.state === "connecting" && `Connecting to ${name}…`}
                    {status.state === "installing" && `Installing the desktop layer on ${name}…`}
                    {status.state === "missing" && `The desktop layer is missing on ${name}.`}
                    {status.state === "disconnected" && "Desktop connection ended."}
                    {status.state === "error" && status.message}
                </p>
                {status.state === "idle" &&
                    <button className="primary" type="button" onClick={() => void run("Connect")}>Connect</button>}
                {status.state === "missing" &&
                    <button className="primary" type="button" onClick={() => void run("Install")}>Install desktop</button>}
                {(status.state === "disconnected" || status.state === "error") &&
                    <button className="primary" type="button" onClick={() => void run("Connect")}>Reconnect</button>}
            </div>}
    </div>;
}
