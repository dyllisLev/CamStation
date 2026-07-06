import { request } from "./http";
import type { BackupSettings, Job } from "./settingsJobsApi";

export type BackupStatus = {
  readonly config: BackupSettings;
  readonly activeJob?: Job | null;
  readonly history: readonly Job[];
  readonly schedule?: {
    readonly enabled: boolean;
    readonly cron: string;
    readonly due: boolean;
    readonly blockedReason?: string;
    readonly lastSucceededAt?: string;
    readonly lastJobUpdatedAt?: string;
    readonly nextRunAt?: string;
  };
};

export type BackupJobsResponse = {
  readonly jobs: readonly Job[];
};

export type BackupStartInput = {
  readonly source?: string;
  readonly prefix?: string;
  readonly timeoutSeconds?: number;
};

export const backupApi = {
  backupConfig: () => request<BackupSettings>("/api/backup/config"),
  updateBackupConfig: (config: BackupSettings) =>
    request<BackupSettings>("/api/backup/config", { method: "PUT", body: JSON.stringify(config) }),
  backupStatus: () => request<BackupStatus>("/api/backup/status"),
  backupJobs: () => request<BackupJobsResponse>("/api/backup/jobs"),
  startBackup: (input: BackupStartInput) =>
    request<Job>("/api/backup/jobs", { method: "POST", body: JSON.stringify(input) }),
  backupJob: (id: number) => request<Job>(`/api/backup/jobs/${id}`),
  cancelBackupJob: (id: number) => request<Job>(`/api/backup/jobs/${id}/cancel`, { method: "POST" }),
  retryBackupJob: (id: number) => request<Job>(`/api/backup/jobs/${id}/retry`, { method: "POST" }),
  deleteBackupJob: (id: number) => request<Job>(`/api/backup/jobs/${id}`, { method: "DELETE" }),
} as const;
