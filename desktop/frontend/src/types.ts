export type VMRef = { kind: "local"; name: string } | { kind: "remote"; alias: string };

export interface RemoteVM {
    id: string;
    name: string;
    host: string;
    state: string;
    hint: string;
}

export interface DoctorResult {
    checked_at: string;
    passed: number;
    failed: number;
    skipped: number;
    failing_checks?: string[];
}

export interface VMStatus {
    name: string;
    state: string;
    uptime_seconds?: number;
    doctor?: DoctorResult;
    provisioning?: { step?: string; error?: string };
    errors?: string[];
}

interface Usage {
    cpu_percent: number;
    memory_bytes?: number;
}

interface Storage {
    total_bytes: number;
    used_bytes: number;
}

export interface LocalStatus extends VMStatus {
    cpus: number;
    host?: Usage;
    hostagent?: Usage;
    guest?: { load?: number[]; memory?: Storage; disk?: Storage };
    agents?: { pid: number; name: string; cwd?: string }[];
    ports?: {
        address: string;
        guest_port: number;
        published: boolean;
        host_url?: string;
        processes?: { pid: number; name: string }[];
    }[];
    denials?: { time: string; msg: string; protocol?: string; destination?: string; reason?: string }[];
}
