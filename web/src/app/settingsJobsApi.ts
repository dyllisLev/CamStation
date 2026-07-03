import { request } from "./http";
import type { JsonObject } from "./http";

export type SecretDisplay = {
  readonly hasSecret: boolean;
  readonly masked: string;
  readonly fingerprint: string;
};

export type RecordingSettings = {
  readonly segmentMinutes: number;
  readonly retentionDays: number;
  readonly maxStorageGB: number;
};

export type BackupSettings = {
  readonly enabled: boolean;
  readonly target: string;
  readonly retentionDays: number;
  readonly scheduleEnabled: boolean;
  readonly scheduleIntervalMinutes: number;
  readonly protectUnbacked: boolean;
};

export type AlertSettings = {
  readonly discordEnabled: boolean;
  readonly discordWebhook: SecretDisplay;
};

export type Settings = {
  readonly recording: RecordingSettings;
  readonly backup: BackupSettings;
  readonly alerts: AlertSettings;
  readonly updatedAt: string;
};

export type AlertSettingsInput = {
  readonly discordEnabled: boolean;
  readonly webhook?: string;
};

export type SettingsUpdate = {
  readonly recording?: RecordingSettings;
  readonly backup?: BackupSettings;
  readonly alerts?: AlertSettingsInput;
};

export type JobState = "queued" | "running" | "succeeded" | "failed" | "cancelled" | "deleted" | string;

export type JobEvent = {
  readonly id: number;
  readonly jobId: number;
  readonly createdAt: string;
  readonly type: string;
  readonly message: string;
  readonly details?: JsonObject;
};

export type Job = {
  readonly id: number;
  readonly kind: string;
  readonly singleFlightKey?: string;
  readonly state: JobState;
  readonly timeoutSeconds?: number;
  readonly error?: string;
  readonly result?: JsonObject;
  readonly createdAt: string;
  readonly startedAt?: string;
  readonly completedAt?: string;
  readonly updatedAt: string;
  readonly events?: readonly JobEvent[];
};

export type JobCreate = {
  readonly kind: string;
  readonly singleFlightKey?: string;
  readonly timeoutSeconds?: number;
};

export type JobResultInput = {
  readonly result?: JsonObject;
};

export type JobFailInput = JobResultInput & {
  readonly error: string;
};

export type JobCancelInput = {
  readonly reason: string;
};

const discordWebhookUrlKey = "discordWebhookUrl" as const;

type AlertSettingsWire = {
  readonly discordEnabled: boolean;
  readonly [discordWebhookUrlKey]?: string;
};

type SettingsUpdateWire = Omit<SettingsUpdate, "alerts"> & {
  readonly alerts?: AlertSettingsWire;
};

function settingsWire(update: SettingsUpdate): SettingsUpdateWire {
  return {
    ...update,
    alerts: update.alerts
      ? { discordEnabled: update.alerts.discordEnabled, [discordWebhookUrlKey]: update.alerts.webhook }
      : undefined,
  };
}

export const settingsJobsApi = {
  settings: () => request<Settings>("/api/settings"),
  updateSettings: (settings: SettingsUpdate) =>
    request<Settings>("/api/settings", { method: "PUT", body: JSON.stringify(settingsWire(settings)) }),
  resetSettings: () => request<Settings>("/api/settings/reset", { method: "POST" }),
  testAlert: () => request<{ readonly ok: boolean; readonly provider: string; readonly webhook: SecretDisplay; readonly sentAt: string }>(
    "/api/settings/test-alert",
    { method: "POST" },
  ),
  jobs: () => request<readonly Job[]>("/api/jobs"),
  createJob: (job: JobCreate) => request<Job>("/api/jobs", { method: "POST", body: JSON.stringify(job) }),
  job: (id: number) => request<Job>(`/api/jobs/${id}`),
  startJob: (id: number) => request<Job>(`/api/jobs/${id}/start`, { method: "POST" }),
  succeedJob: (id: number, input: JobResultInput) =>
    request<Job>(`/api/jobs/${id}/succeed`, { method: "POST", body: JSON.stringify(input) }),
  failJob: (id: number, input: JobFailInput) =>
    request<Job>(`/api/jobs/${id}/fail`, { method: "POST", body: JSON.stringify(input) }),
  cancelJob: (id: number, input: JobCancelInput) =>
    request<Job>(`/api/jobs/${id}/cancel`, { method: "POST", body: JSON.stringify(input) }),
  deleteJob: (id: number) => request<Job>(`/api/jobs/${id}`, { method: "DELETE" }),
} as const;
