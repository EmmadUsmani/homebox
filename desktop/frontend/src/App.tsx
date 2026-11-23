import { useCallback, useEffect, useState } from "react";
import { Call } from "@wailsio/runtime";

export default function App() {
    const [pending, setPending] = useState(true);
    const [reads, setReads] = useState(0);
    const [result, setResult] = useState("");
    const [failed, setFailed] = useState(false);

    const refresh = useCallback(async () => {
        setPending(true);
        try {
            const json = await Call.ByName("main.Status.ListStatus");
            setResult(json);
            setReads((count) => count + 1);
            setFailed(false);
        } catch (error) {
            setResult(String(error));
            setFailed(true);
        } finally {
            setPending(false);
        }
    }, []);

    useEffect(() => { void refresh(); }, [refresh]);

    let status = `Status loaded (${reads})`;
    if (pending) status = "Loading status";
    else if (failed) status = "Status failed";

    return (
        <main>
            <button type="button" disabled={pending} onClick={refresh}>Refresh status</button>
            <p role="status">{status}</p>
            <pre>{result}</pre>
        </main>
    );
}
