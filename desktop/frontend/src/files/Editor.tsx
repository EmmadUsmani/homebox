/// <reference types="vite/client" />
import { useEffect, useRef } from "react";
import { Clipboard } from "@wailsio/runtime";
import * as monaco from "monaco-editor/editor/editor.api.js";
import "monaco-editor/basic-languages/monaco.contribution.js";
import "monaco-editor/editor/contrib/find/browser/findController.js";
import "monaco-editor/editor/contrib/gotoError/browser/gotoError.js";
import "monaco-editor/language/json/monaco.contribution.js";
import EditorWorker from "monaco-editor/editor/editor.worker.js?worker&inline";
import JSONWorker from "monaco-editor/language/json/json.worker.js?worker&inline";

globalThis.MonacoEnvironment = {
    getWorker: (_moduleId, label) => label === "json" ? new JSONWorker() : new EditorWorker(),
};

monaco.editor.defineTheme("homebox", {
    base: "vs-dark", inherit: true, rules: [],
    colors: {
        "editor.background": "#0b0d11",
        "editor.foreground": "#e6e8ee",
        "editorLineNumber.foreground": "#788194",
        "editorCursor.foreground": "#e6e8ee",
        "editor.selectionBackground": "#304366",
        "editor.lineHighlightBackground": "#151821",
    },
});

export default function Editor({ path, initial, onChange, onSave }: {
    path: string; initial: string; onChange: (text: string) => void; onSave: () => void;
}) {
    const host = useRef<HTMLDivElement>(null);
    const callbacks = useRef({ onChange, onSave });
    callbacks.current = { onChange, onSave };

    useEffect(() => {
        const model = monaco.editor.createModel(initial, undefined, monaco.Uri.from({ scheme: "homebox", path }));
        const editor = monaco.editor.create(host.current!, {
            model, theme: "homebox", automaticLayout: true, minimap: { enabled: false },
            fontFamily: '"JetBrains Mono", monospace', fontSize: 13, lineHeight: 21,
            padding: { top: 12 }, scrollBeyondLastLine: false, links: false,
            quickSuggestions: false, accessibilitySupport: "on", ariaLabel: "File editor",
        });
        const change = model.onDidChangeContent(() => callbacks.current.onChange(model.getValue(monaco.editor.EndOfLinePreference.TextDefined, true)));
        editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => callbacks.current.onSave());
        const clipboard = editor.onKeyDown((event) => {
            if (!event.metaKey || event.altKey || event.ctrlKey || event.shiftKey) return;
            if (event.keyCode === monaco.KeyCode.KeyC) {
                const selected = editor.getSelection();
                if (!selected || selected.isEmpty()) return;
                event.preventDefault();
                event.stopPropagation();
                void Clipboard.SetText(model.getValueInRange(selected)).catch(console.error);
            }
            if (event.keyCode === monaco.KeyCode.KeyV) {
                event.preventDefault();
                event.stopPropagation();
                void Clipboard.Text().then((text) => {
                    if (!model.isDisposed() && editor.hasTextFocus()) editor.trigger("clipboard", "type", { text });
                }).catch(console.error);
            }
        });
        editor.focus();
        return () => {
            clipboard.dispose();
            change.dispose();
            editor.dispose();
            model.dispose();
        };
    }, [initial, path]);

    return <div className="files-monaco" ref={host} />;
}
