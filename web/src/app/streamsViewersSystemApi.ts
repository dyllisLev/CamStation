import { request } from "./http";
import type { Job } from "./settingsJobsApi";

export type StreamRuntime = {
  readonly state: string;
  readonly producerCount: number;
  readonly consumerCount: number;
  readonly viewerCount: number;
};

export type StreamStatus = {
  readonly installed: boolean;
  readonly running: boolean;
  readonly error?: string;
  readonly streams?: Readonly<Record<string, StreamRuntime>>;
};

export type StreamProbe = {
  readonly reachable: boolean;
  readonly format?: string;
  readonly streams: number;
  readonly failure?: string;
  readonly checkedAt: string;
};

export type StreamOperationResponse = {
  readonly ok: boolean;
  readonly operation: "restart" | "probe" | "delete_rejected" | string;
  readonly streamName: string;
  readonly cameraName: string;
  readonly status: StreamStatus;
  readonly probe?: StreamProbe;
  readonly message?: string;
};

export type ViewerStreamHealth = {
  readonly streamName: string;
  readonly state: string;
  readonly transport?: string;
  readonly latencyMs?: number;
  readonly lastBinaryAt?: string;
  readonly lastProgressAt?: string;
  readonly updatedAt?: string;
};

export type ViewerAgentHealth = {
  readonly state: string;
  readonly version?: string;
};

export type ViewerControlHealth = {
  readonly state: string;
  readonly lastSuccessAt?: string;
};

export type ViewerProcessHealth = {
  readonly state: string;
  readonly version?: string;
  readonly lastHeartbeatAt?: string;
};

export type ViewerRendererHealth = {
  readonly state: string;
  readonly lastHeartbeatAt?: string;
  readonly lastProgressAt?: string;
};

export type ViewerUpdateHealth = {
  readonly state: string;
  readonly targetVersion?: string;
  readonly generation: number;
};

export type ViewerHeartbeat = {
  readonly id: string;
  readonly displayName: string;
  readonly appVersion: string;
  readonly hostname: string;
  readonly deviceLabel: string;
  readonly route: string;
  readonly mode: string;
  readonly agent?: ViewerAgentHealth;
  readonly control?: ViewerControlHealth;
  readonly viewer?: ViewerProcessHealth;
  readonly renderer?: ViewerRendererHealth;
  readonly update?: ViewerUpdateHealth;
  readonly streams?: readonly ViewerStreamHealth[];
};

export type ViewerUpdate = {
  readonly label?: string;
  readonly status?: string;
  readonly note?: string;
};

export type Viewer = ViewerHeartbeat & {
  readonly label?: string;
  readonly status: string;
  readonly note?: string;
  readonly createdAt: string;
  readonly lastHeartbeatAt: string;
  readonly updatedAt: string;
};

export type ViewerDesiredRelease = {
  readonly version: string;
  readonly filename: string;
  readonly sizeBytes: number;
  readonly sha256: string;
  readonly publishedAt: string;
  readonly developmentUnsigned: boolean;
  readonly downloadUrl: string;
  readonly generation: number;
};

export type ViewerHeartbeatResponse = {
  readonly viewer: Viewer;
  readonly desiredRelease: ViewerDesiredRelease | null;
  readonly commitToken?: string;
};

export type ViewerCommandState =
  | "pending"
  | "delivered"
  | "acknowledged"
  | "running"
  | "succeeded"
  | "failed"
  | "rejected"
  | "expired"
  | "cancelled"
  | "deleted";

export type ViewerCommand = {
  readonly id: number;
  readonly viewerId: string;
  readonly type: string;
  readonly message?: string;
  readonly route?: string;
  readonly mode?: string;
  readonly streamName?: string;
  readonly desiredVersion?: string;
  readonly artifactSha256?: string;
  readonly payloadHash: string;
  readonly ttlSeconds: number;
  readonly operationKey?: string;
  readonly generation: number;
  readonly state: ViewerCommandState;
  readonly error?: string;
  readonly createdAt: string;
  readonly sentAt?: string;
  readonly deliveredAt?: string;
  readonly acknowledgedAt?: string;
  readonly runningAt?: string;
  readonly resultAt?: string;
  readonly completedAt?: string;
  readonly updatedAt: string;
};

export type ViewerCommandInput = {
  readonly type: string;
  readonly message?: string;
  readonly route?: string;
  readonly mode?: string;
  readonly streamName?: string;
  readonly desiredVersion?: string;
  readonly artifactSha256?: string;
  readonly ttlSeconds?: number;
  readonly generation?: number;
};

export type ViewerCommandUpdate = {
  readonly state: "acknowledged" | "running" | "succeeded" | "failed" | "rejected" | "expired";
  readonly error?: string;
  readonly operationKey?: string;
};

export type SystemStatus = {
  readonly daemon: { readonly running: boolean; readonly now: string };
  readonly go2rtc: StreamStatus;
  readonly ffmpeg: { readonly installed: boolean; readonly error?: string };
  readonly system: { readonly goos: string; readonly goarch: string; readonly cpus: number; readonly goroutines: number };
};

export type DiagnosticArtifact = {
  readonly id: number;
  readonly jobId: number;
  readonly name: string;
  readonly sizeBytes: number;
  readonly sha256: string;
  readonly createdAt: string;
  readonly deletedAt?: string;
};

export type DiagnosticResponse = {
  readonly job: Job;
  readonly artifact: DiagnosticArtifact;
};

export type MaintenanceInput = {
  readonly action: "health_check" | "db_vacuum" | "recording_cleanup";
  readonly defer?: boolean;
  readonly maxBytes?: number;
};

export const streamsViewersSystemApi = {
  streamStatus: () => request<StreamStatus>("/api/streams/status"),
  restartStreams: () => request<StreamStatus>("/api/streams/restart", { method: "POST" }),
  restartStream: (streamName: string) =>
    request<StreamOperationResponse>(`/api/streams/${encodeURIComponent(streamName)}/restart`, { method: "POST" }),
  probeStream: (streamName: string) =>
    request<StreamOperationResponse>(`/api/streams/${encodeURIComponent(streamName)}/probe`, { method: "POST" }),
  rejectStreamDelete: (streamName: string) =>
    request<{ readonly error: string; readonly streamName: string }>(`/api/streams/${encodeURIComponent(streamName)}`, {
      method: "DELETE",
    }),
  viewerHeartbeat: (heartbeat: ViewerHeartbeat) =>
    request<ViewerHeartbeatResponse>("/api/viewers/heartbeat", { method: "POST", body: JSON.stringify(heartbeat) }),
  viewers: () => request<readonly Viewer[]>("/api/viewers"),
  updateViewer: (id: string, viewer: ViewerUpdate) =>
    request<Viewer>(`/api/viewers/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(viewer) }),
  deleteViewer: (id: string) =>
    request<{ readonly deleted: boolean; readonly id: string }>(`/api/viewers/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),
  createViewerCommand: (id: string, command: ViewerCommandInput) =>
    request<ViewerCommand>(`/api/viewers/${encodeURIComponent(id)}/commands`, {
      method: "POST",
      body: JSON.stringify(command),
    }),
  viewerCommands: (id: string) => request<readonly ViewerCommand[]>(`/api/viewers/${encodeURIComponent(id)}/commands`),
  updateViewerCommand: (id: string, commandID: number, command: ViewerCommandUpdate) =>
    request<ViewerCommand>(`/api/viewers/${encodeURIComponent(id)}/commands/${commandID}`, {
      method: "PATCH",
      body: JSON.stringify(command),
    }),
  cancelViewerCommand: (id: string, commandID: number, reason: string) =>
    request<ViewerCommand>(`/api/viewers/${encodeURIComponent(id)}/commands/${commandID}/cancel`, {
      method: "POST",
      body: JSON.stringify({ reason }),
    }),
  deleteViewerCommand: (id: string, commandID: number) =>
    request<ViewerCommand>(`/api/viewers/${encodeURIComponent(id)}/commands/${commandID}`, { method: "DELETE" }),
  systemStatus: () => request<SystemStatus>("/api/system/status"),
  createDiagnostic: (reason: string) =>
    request<DiagnosticResponse>("/api/system/diagnostics", { method: "POST", body: JSON.stringify({ reason }) }),
  systemJobs: () => request<readonly Job[]>("/api/system/jobs"),
  createMaintenance: (input: MaintenanceInput) =>
    request<Job>("/api/system/maintenance", { method: "POST", body: JSON.stringify(input) }),
  cancelSystemJob: (id: number) => request<Job>(`/api/system/jobs/${id}/cancel`, { method: "POST" }),
  diagnosticArtifacts: () => request<readonly DiagnosticArtifact[]>("/api/system/diagnostics/artifacts"),
  deleteDiagnosticArtifact: (id: number) =>
    request<{ readonly deleted: boolean; readonly artifact: DiagnosticArtifact }>(
      `/api/system/diagnostics/artifacts/${id}`,
      { method: "DELETE" },
    ),
  deleteDiagnosticHistory: () =>
    request<{ readonly deleted: number }>("/api/system/diagnostics/history", { method: "DELETE" }),
} as const;
