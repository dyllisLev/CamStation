import { queryString, request } from "./http";
import type {
  Camera,
  CameraPreviewRequest,
  CameraPreviewResponse,
  CameraScanRequest,
  CreateCamera,
  DeviceProfile,
  Health,
  LayoutProfile,
  TimelineData,
  UpdateCamera,
} from "./cameraTypes";
import type { StreamStatus } from "./streamsViewersSystemApi";

type CameraMutationResponse = {
  readonly ok: boolean;
  readonly camera: Camera;
  readonly go2rtc: StreamStatus;
  readonly warning?: string;
};

type LayoutInput = Pick<LayoutProfile, "name" | "data" | "timeline_collapsed" | "grid_cols" | "grid_rows">;

export const cameraApi = {
  health: () => request<Health>("/api/health"),
  cameras: () => request<Camera[]>("/api/cameras"),
  layouts: () => request<LayoutProfile[]>("/api/layouts"),
  createLayout: (layout: LayoutInput) =>
    request<LayoutProfile>("/api/layouts", { method: "POST", body: JSON.stringify(layout) }),
  updateLayout: (id: string, layout: Partial<LayoutInput>) =>
    request<LayoutProfile>(`/api/layouts/${encodeURIComponent(id)}`, {
      method: "PUT",
      body: JSON.stringify(layout),
    }),
  timeline: (camera: string, date: string) =>
    request<TimelineData>(`/api/timeline${queryString({ cam: camera, date })}`),
  scanCamera: (camera: CameraScanRequest) =>
    request<{ readonly ok: boolean; readonly profile: DeviceProfile }>("/api/cameras/scan", {
      method: "POST",
      body: JSON.stringify(camera),
    }),
  previewCamera: (camera: CameraPreviewRequest) =>
    request<CameraPreviewResponse>("/api/cameras/preview", {
      method: "POST",
      body: JSON.stringify(camera),
    }),
  scanRegisteredCamera: (streamName: string, camera: CameraScanRequest) =>
    request<{ readonly ok: boolean; readonly profile: DeviceProfile }>(
      `/api/cameras/${encodeURIComponent(streamName)}/scan`,
      { method: "POST", body: JSON.stringify(camera) },
    ),
  previewRegisteredCamera: (streamName: string, camera: CameraPreviewRequest) =>
    request<CameraPreviewResponse>(`/api/cameras/${encodeURIComponent(streamName)}/preview`, {
      method: "POST",
      body: JSON.stringify(camera),
    }),
  createCamera: (camera: CreateCamera) =>
    request<CameraMutationResponse>("/api/cameras", { method: "POST", body: JSON.stringify(camera) }),
  updateCamera: (streamName: string, camera: UpdateCamera) =>
    request<CameraMutationResponse>(`/api/cameras/${encodeURIComponent(streamName)}`, {
      method: "PUT",
      body: JSON.stringify(camera),
    }),
  deleteCamera: (streamName: string) =>
    request<CameraMutationResponse>(`/api/cameras/${encodeURIComponent(streamName)}`, { method: "DELETE" }),
} as const;
