export type VMRef = { kind: "local"; name: string } | { kind: "remote"; alias: string };

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

export interface VMDetail extends VMStatus {
    guest?: {
        load?: number[];
        memory?: Storage;
        disk?: Storage;
        processes?: { pid: number; name: string; cpu_percent: number }[];
    };
    containers?: { id: string; name: string; image: string; status: string }[];
    agents?: { pid: number; name: string; cwd?: string }[];
    ports?: {
        address: string;
        guest_port: number;
        published: boolean;
        host_url?: string;
        processes?: { pid: number; name: string }[];
    }[];
}

export interface LocalStatus extends VMDetail {
    cpus: number;
    memory_gib: number;
    host?: Usage;
    hostagent?: Usage;
    denials?: { time: string; msg: string; protocol?: string; destination?: string; reason?: string }[];
}
