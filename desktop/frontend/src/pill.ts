const states: Record<string, { label: string; tone: string }> = {
    running: { label: "Running", tone: "good" },
    stopped: { label: "Stopped", tone: "idle" },
    broken: { label: "Broken", tone: "bad" },
    provisioning: { label: "Provisioning", tone: "warn" },
    "provision-failed": { label: "Provisioning failed", tone: "bad" },
    "provision-interrupted": { label: "Provisioning interrupted", tone: "warn" },
    reachable: { label: "Reachable", tone: "good" },
    unreachable: { label: "Unreachable", tone: "bad" },
    "connection-broken": { label: "Connection broken", tone: "warn" },
    "not-running": { label: "VM not running", tone: "idle" },
    "host-key-changed": { label: "Key changed", tone: "warn" },
};

export function statePill(state: string) {
    return Object.prototype.hasOwnProperty.call(states, state) ? states[state] : { label: state || "Unknown", tone: "idle" };
}
