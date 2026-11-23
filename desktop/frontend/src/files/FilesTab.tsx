import { lazy, Suspense, useEffect, useState } from "react";
import { Call, Dialogs } from "@wailsio/runtime";
import type { VMRef } from "../types";
import "./files.css";

const Editor = lazy(() => import("./Editor"));
const service = "github.com/EmmadUsmani/homebox/desktop/session.Files.";
const call = <T,>(method: string, ...args: unknown[]) => Call.ByName(service + method, ...args) as Promise<T>;

type View = { id: string; home: string };
type Stamp = { size: number; modified: number };
type Entry = Stamp & { name: string; kind: "folder" | "file" | "link" | "other" };
type Listing = { entries: Entry[]; more: boolean };
type FileContent = { text: string; stamp: Stamp };
type SaveResult = { stamp: Stamp; conflict: boolean };
type Transfer = { active: boolean; bytes: number; total: number };

const date = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function size(bytes: number) {
    if (bytes < 1024) return bytes + " B";
    if (bytes < 1024 ** 2) return (bytes / 1024).toFixed(1) + " KB";
    if (bytes < 1024 ** 3) return (bytes / 1024 ** 2).toFixed(1) + " MB";
    return (bytes / 1024 ** 3).toFixed(1) + " GB";
}

export async function confirmDiscard() {
    return await Dialogs.Question({
        Title: "Discard unsaved changes?",
        Message: "Leave this file and discard your edits?",
        Buttons: [{ Label: "Keep editing", IsCancel: true, IsDefault: true }, { Label: "Discard changes" }],
    }) === "Discard changes";
}

export function FilesTab({ vm, onDirtyChange }: { vm: VMRef; onDirtyChange?: (dirty: boolean) => void }) {
    const [view, setView] = useState<View>();
    const [attempt, setAttempt] = useState(0);
    const [directory, setDirectory] = useState("");
    const [goto, setGoto] = useState("");
    const [hidden, setHidden] = useState(false);
    const [listing, setListing] = useState<Listing>();
    const [revision, setRevision] = useState(0);
    const [target, setTarget] = useState("");
    const [opened, setOpened] = useState<FileContent>();
    const [savedText, setSavedText] = useState("");
    const [text, setText] = useState("");
    const [connectionError, setConnectionError] = useState("");
    const [listingError, setListingError] = useState("");
    const [editorError, setEditorError] = useState("");
    const [transferError, setTransferError] = useState("");
    const [conflict, setConflict] = useState(false);
    const [compare, setCompare] = useState<FileContent>();
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [transfer, setTransfer] = useState<Transfer>();
    const vmKind = vm.kind;
    const vmName = vm.kind === "local" ? vm.name : vm.alias;
    const dirty = !!opened && text !== savedText;
    const ready = !!view && !loading && !saving;

    useEffect(() => {
        onDirtyChange?.(dirty);
        return () => onDirtyChange?.(false);
    }, [dirty, onDirtyChange]);

    useEffect(() => {
        if (!dirty) return;
        const confirm = (event: BeforeUnloadEvent) => event.preventDefault();
        window.addEventListener("beforeunload", confirm);
        return () => window.removeEventListener("beforeunload", confirm);
    }, [dirty]);

    useEffect(() => {
        let active = true;
        let id: string | undefined;
        setConnectionError("");
        const ref = vmKind === "local" ? { kind: vmKind, name: vmName } : { kind: vmKind, alias: vmName };
        void call<View>("Open", ref).then((result) => {
            id = result.id;
            if (active) {
                setView(result);
                setDirectory(result.home);
                setGoto(result.home);
            } else {
                void call("Close", id);
            }
        }).catch((error) => { if (active) setConnectionError(String(error)); });
        return () => {
            active = false;
            if (id) void call("Close", id);
        };
    }, [vmKind, vmName, attempt]);

    useEffect(() => {
        if (!view || !directory) return;
        let active = true;
        setListingError("");
        void call<Listing>("List", view.id, directory, hidden).then((result) => {
            if (active) setListing(result);
        }).catch((error) => { if (active) setListingError(String(error)); });
        return () => { active = false; };
    }, [view, directory, hidden, revision]);

    async function allowLeave() {
        return !dirty || confirmDiscard();
    }

    function clearEditor() {
        setTarget("");
        setOpened(undefined);
        setEditorError("");
        setConflict(false);
        setCompare(undefined);
    }

    async function browse(path: string) {
        if (!ready || path === directory || !await allowLeave()) return;
        clearEditor();
        setListing(undefined);
        setDirectory(path);
        setGoto(path);
    }

    async function open(path: string) {
        if (!view || !ready || target === path || !await allowLeave()) return;
        clearEditor();
        setTarget(path);
        setLoading(true);
        try {
            const file = await call<FileContent>("Read", view.id, path);
            setOpened(file);
            setSavedText(file.text);
            setText(file.text);
        } catch (error) {
            setEditorError(String(error));
        } finally {
            setLoading(false);
        }
    }

    async function save() {
        if (!view || !opened || !dirty || !ready || conflict) return;
        setSaving(true);
        setEditorError("");
        try {
            const result = await call<SaveResult>("Save", view.id, target, opened.stamp, text);
            setConflict(result.conflict);
            if (!result.conflict) {
                setOpened({ ...opened, stamp: result.stamp });
                setSavedText(text);
                setRevision((value) => value + 1);
            }
        } catch (error) {
            setEditorError(String(error));
        } finally {
            setSaving(false);
        }
    }

    async function reloadAndCompare() {
        if (!view) return;
        setEditorError("");
        try {
            setCompare(await call<FileContent>("Read", view.id, target));
        } catch (error) {
            setEditorError(String(error));
        }
    }

    async function transferFile(method: "Upload" | "Download") {
        if (!view || transfer) return;
        setTransferError("");
        setTransfer({ active: true, bytes: 0, total: 0 });
        let active = true;
        const poll = window.setInterval(() => {
            void call<Transfer>("TransferStatus", view.id).then((result) => {
                if (active && result.active) setTransfer(result);
            }).catch(() => {});
        }, 250);
        try {
            await call(method, view.id, method === "Upload" ? directory : target);
            if (method === "Upload") setRevision((value) => value + 1);
        } catch (error) {
            if (!String(error).includes("context canceled")) setTransferError(String(error));
        } finally {
            active = false;
            window.clearInterval(poll);
            setTransfer(undefined);
        }
    }

    function displayPath(path: string) {
        if (!view) return path;
        return path === view.home || path.startsWith(view.home + "/") ? "~" + path.slice(view.home.length) : path;
    }

    const parent = directory.slice(0, directory.lastIndexOf("/")) || "/";

    return <div className="files-layout">
        <section className="files-browser" aria-label="VM files">
            <div className="files-tools">
                <button disabled={!ready || !!transfer} onClick={() => void transferFile("Upload")}>Upload</button>
                <button disabled={!ready || !target || !!transfer} onClick={() => void transferFile("Download")}>Download</button>
            </div>
            <form className="files-path" onSubmit={(event) => { event.preventDefault(); void browse(goto); }}>
                <input aria-label="VM folder path" disabled={!ready} value={goto} onChange={(event) => setGoto(event.target.value)} maxLength={4096} />
                <button disabled={!ready || !goto}>Go</button>
            </form>
            <div className="files-tools files-navigation">
                <button disabled={!ready || directory === view?.home} onClick={() => void browse(view!.home)}>Home</button>
                <button disabled={!ready || directory === "/"} onClick={() => void browse(parent)}>Up</button>
                <button disabled={!ready} onClick={() => setRevision((value) => value + 1)}>Refresh</button>
            </div>
            <label className="files-hidden"><input type="checkbox" checked={hidden} onChange={(event) => setHidden(event.target.checked)} /> Show hidden files</label>
            {transfer && <div className="files-progress" role="status">
                <progress aria-label="Transfer progress" max={transfer.total || undefined} value={transfer.total ? transfer.bytes : undefined} />
                <span>{size(transfer.bytes)}{transfer.total > 0 && " / " + size(transfer.total)}</span>
                <button onClick={() => void call("CancelTransfer", view!.id)}>Cancel</button>
            </div>}
            {transferError && <p className="notice" role="alert">{transferError}</p>}
            {connectionError && <div className="files-message" role="alert"><p>{connectionError}</p>
                <button onClick={() => setAttempt((value) => value + 1)}>Reconnect</button>
            </div>}
            {listingError && <p className="notice" role="alert">{listingError}</p>}
            {!listing && !connectionError && !listingError && <p className="files-message muted" role="status">{view ? "Loading folder…" : "Connecting to the VM…"}</p>}
            {listing?.more && <p className="files-message muted" role="status">Showing the first 5,000 directory entries.</p>}
            {listing?.entries.length === 0 && <p className="files-message muted">No visible files</p>}
            <ul className="files-rows" aria-label={directory ? "Files in " + displayPath(directory) : "Files"}>
                {listing?.entries.map((entry) => {
                    const path = (directory === "/" ? "" : directory) + "/" + entry.name;
                    return <li key={entry.name}>
                        <button className="files-row" aria-label={entry.name} aria-current={path === target}
                            disabled={!ready || (entry.kind !== "folder" && entry.kind !== "file")}
                            title={entry.name + " · " + date.format(entry.modified * 1000)}
                            onClick={() => void (entry.kind === "folder" ? browse(path) : open(path))}>
                            <span className="files-kind" aria-hidden="true">{entry.kind === "folder" ? "▸" : entry.kind === "link" ? "↗" : "·"}</span>
                            <span className="files-name">{entry.name}</span>
                            <span className="muted">{entry.kind === "file" ? size(entry.size) : ""}</span>
                            <span className="muted files-date">{date.format(entry.modified * 1000)}</span>
                        </button>
                    </li>;
                })}
            </ul>
        </section>
        <section className="files-editor" aria-label="Editor">
            {target && <header className="files-editor-header">
                <code title={target}>{displayPath(target)}</code>
                {dirty && <span className="warning files-edited">● edited</span>}
                <span className="spacer" />
                {opened && <span className="muted">{size(opened.stamp.size)} · {date.format(opened.stamp.modified * 1000)}</span>}
                <button className="primary" disabled={!dirty || !ready || conflict} onClick={() => void save()}>{saving ? "Saving…" : "Save"}</button>
            </header>}
            {!target && <p className="files-message muted">Select a file to open it.</p>}
            {loading && <p className="files-message muted" role="status">Opening file…</p>}
            {opened && <Suspense fallback={<p className="files-message muted">Loading editor…</p>}>
                <Editor path={target} initial={opened.text} onChange={setText} onSave={() => void save()} />
            </Suspense>}
            {editorError && <p className="files-message notice" role="alert">{editorError}</p>}
            {compare && <div className="files-compare">
                <div><strong>Current VM file</strong><button onClick={() => {
                    setOpened({ text, stamp: compare.stamp });
                    setSavedText(compare.text);
                    setConflict(false);
                    setCompare(undefined);
                }}>Keep my edits against this version</button></div>
                <pre>{compare.text}</pre>
            </div>}
            {conflict && <div className="files-conflict" role="status">
                This file changed in the VM since you opened it. Saving is blocked so nothing is overwritten.{" "}
                <button className="text-button" onClick={() => void reloadAndCompare()}>Reload and compare</button>
            </div>}
        </section>
    </div>;
}
