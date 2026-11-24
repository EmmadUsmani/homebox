import { useEffect, useRef, useState } from "react";
import { Call, Events } from "@wailsio/runtime";
import { HostKeyWarning } from "../terminal/TerminalTab";
import type { VMRef } from "../types";
import "./desktop.css";

type Result = {
    state: "idle" | "connecting" | "connected" | "installing" | "missing" | "disconnected" | "keyChanged" | "error";
    message?: string;
    id?: string;
};

type Pending = ReturnType<typeof Call.ByName>;
const call = (method: string, ...args: unknown[]) => Call.ByName("main.Desktop." + method, ...args);

let nextAttempt = 0;

function bounds(element: HTMLElement) {
    const { x, y, width, height } = element.getBoundingClientRect();
    return { X: x, Y: y, Width: width, Height: height };
}

export function DesktopTab({ vm, openSSHSettings }: { vm: VMRef; openSSHSettings?: () => void }) {
    const viewport = useRef<HTMLDivElement>(null);
    const pending = useRef<Pending>();
    const clipboardRequest = useRef<Pending>();
    const [clipboardMessage, setClipboardMessage] = useState("");
    const [clipboardBusy, setClipboardBusy] = useState(false);
    const [status, setStatus] = useState<Result>({ state: "idle" });
    const name = vm.kind === "local" ? vm.name : vm.alias;

    useEffect(() => () => {
        const request = pending.current;
        pending.current = undefined;
        if (request) void request.cancel();
        const transfer = clipboardRequest.current;
        clipboardRequest.current = undefined;
        if (transfer) void transfer.cancel();
    }, []);

    async function run(method: "Connect" | "Install") {
        if (pending.current || !viewport.current) return;
        const element = viewport.current;
        const id = String(++nextAttempt);
        const resize = () => void call("Resize", id, bounds(element)).catch(console.error);
        const observer = new ResizeObserver(resize);
        const unsubscribe = Events.On("desktop:connected", ({ data }) => {
            if (data !== id || pending.current !== request) return;
            setStatus({ state: "connected", id });
            resize();
        });
        setClipboardMessage("");
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

    async function transfer(method: "PasteToVM" | "CopyFromVM") {
        if (!status.id || clipboardRequest.current) return;
        const request = call(method, status.id);
        clipboardRequest.current = request;
        setClipboardMessage("");
        setClipboardBusy(true);
        try {
            await request;
            if (clipboardRequest.current === request) {
                setClipboardMessage(method === "PasteToVM" ? "Text sent to VM clipboard." : "Text copied to Mac clipboard.");
            }
        } catch (error) {
            if (clipboardRequest.current === request) setClipboardMessage(String(error));
        } finally {
            if (clipboardRequest.current === request) {
                clipboardRequest.current = undefined;
                setClipboardBusy(false);
            }
        }
    }

    return <div className="desktop-tab">
        {status.state === "connected" && <div className="desktop-toolbar" role="group" aria-label="Desktop clipboard">
            <button type="button" disabled={clipboardBusy} onClick={() => void transfer("PasteToVM")}>Paste to VM</button>
            <button type="button" disabled={clipboardBusy} onClick={() => void transfer("CopyFromVM")}>Copy from VM</button>
            <span role="status">{clipboardMessage}</span>
        </div>}
        <div ref={viewport} className="terminal-body desktop-pane" aria-label="Remote desktop">
            {status.state === "keyChanged" && <HostKeyWarning vm={vm} openSSHSettings={openSSHSettings} />}
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
        </div>
    </div>;
}
