import { queryString, request } from "./http";

export type RecorderWorker = {
  readonly streamName: string;
  readonly camera_id: number;
  readonly state: string;
  readonly input: string;
  readonly current?: string;
  readonly lastError?: string;
};

export type RecorderStatus = {
  readonly enabled: boolean;
  readonly recordingsDir: string;
  readonly tempDir: string;
  readonly segmentMinutes: number;
  readonly workers: readonly RecorderWorker[];
};

export type RecordingStorage = {
  readonly recordingsDir: string;
  readonly tempDir: string;
  readonly recordingsBytes: number;
  readonly tempBytes: number;
  readonly maxBytes: number;
  readonly autoCleanupEnabled: boolean;
};

export type CleanupResult = {
  readonly maxBytes: number;
  readonly beforeBytes: number;
  readonly afterBytes: number;
  readonly backupProtectionActive: boolean;
  readonly protectedUnbackedCount: number;
  readonly protectedUnbackedBytes: number;
  readonly deleted: readonly {
    readonly id: number;
    readonly streamName: string;
    readonly filename: string;
    readonly bytes: number;
  }[];
};

export type RecordingSegment = {
  readonly id: number;
  readonly camera_id: number;
  readonly streamName: string;
  readonly filename: string;
  readonly ts_start: number;
  readonly ts_end: number | null;
  readonly file_size: number | null;
  readonly status: string;
  readonly backupState: string;
  readonly backedUpAt?: string;
  readonly backupJobId?: number;
  readonly created_at: number;
  readonly updated_at: number;
  readonly playUrl?: string;
  readonly downloadUrl?: string;
};

export type RecordingSegmentFilter = {
  readonly stream?: string;
  readonly status?: readonly string[];
  readonly from?: string | number;
  readonly to?: string | number;
  readonly limit?: number;
};

export type RecordingSegmentsResponse = {
  readonly segments: readonly RecordingSegment[];
};

export type RecorderControlInput = {
  readonly stream?: string;
};

export const recordingsApi = {
  recorderStatus: () => request<RecorderStatus>("/api/recorders/status"),
  startRecorder: (input: RecorderControlInput = {}) =>
    request<RecorderStatus>(`/api/recorders/start${queryString({ stream: input.stream })}`, { method: "POST" }),
  stopRecorder: (input: RecorderControlInput = {}) =>
    request<RecorderStatus>(`/api/recorders/stop${queryString({ stream: input.stream })}`, { method: "POST" }),
  recordingStorage: () => request<RecordingStorage>("/api/recordings/storage"),
  cleanupRecordings: (maxBytes: number) =>
    request<CleanupResult>("/api/recordings/cleanup", { method: "POST", body: JSON.stringify({ maxBytes }) }),
  recordingSegments: (filter: RecordingSegmentFilter = {}) =>
    request<RecordingSegmentsResponse>(
      `/api/recordings/segments${queryString({
        stream: filter.stream,
        status: filter.status,
        from: filter.from,
        to: filter.to,
        limit: filter.limit,
      })}`,
    ),
  recordingSegment: (id: number) => request<RecordingSegment>(`/api/recordings/segments/${id}`),
  recordingSegmentPlayUrl: (id: number) => `/api/recordings/segments/${id}/play`,
  recordingSegmentDownloadUrl: (id: number) => `/api/recordings/segments/${id}/download`,
  deleteRecordingSegment: (id: number) =>
    request<RecordingSegment>(`/api/recordings/segments/${id}`, { method: "DELETE" }),
} as const;
