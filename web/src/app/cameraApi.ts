import { queryString, request } from "./http";
import type {
  Camera,
  CameraProfileTemplate,
  CameraProfileTemplateInput,
  CameraScanResponse,
  CameraPreviewRequest,
  CameraPreviewResponse,
  CameraScanRequest,
  BulkStreamOutputProbeResponse,
  CreateCamera,
  Health,
  LayoutProfile,
  TimelineData,
  StreamOutputMutationResponse,
  UpdateStreamOutputsRequest,
  UpdateCamera,
} from "./cameraTypes";
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
    request<CameraScanResponse>("/api/cameras/scan", {
      method: "POST",
      body: JSON.stringify(camera),
    }),
  previewCamera: (camera: CameraPreviewRequest) =>
    request<CameraPreviewResponse>("/api/cameras/preview", {
      method: "POST",
      body: JSON.stringify(camera),
    }),
  scanRegisteredCamera: (streamName: string, camera: CameraScanRequest) =>
    request<CameraScanResponse>(
      `/api/cameras/${encodeURIComponent(streamName)}/scan`,
      { method: "POST", body: JSON.stringify(camera) },
    ),
  previewRegisteredCamera: (streamName: string, camera: CameraPreviewRequest) =>
    request<CameraPreviewResponse>(`/api/cameras/${encodeURIComponent(streamName)}/preview`, {
      method: "POST",
      body: JSON.stringify(camera),
    }),
  createCamera: (camera: CreateCamera) =>
    request<StreamOutputMutationResponse>("/api/cameras", { method: "POST", body: JSON.stringify(camera) }),
  updateCamera: (streamName: string, camera: UpdateCamera) =>
    request<StreamOutputMutationResponse>(`/api/cameras/${encodeURIComponent(streamName)}`, {
      method: "PUT",
      body: JSON.stringify(camera),
    }),
  deleteCamera: (streamName: string) =>
    request<StreamOutputMutationResponse>(`/api/cameras/${encodeURIComponent(streamName)}`, { method: "DELETE" }),
  updateStreamOutputs: (streamName: string, input: UpdateStreamOutputsRequest) =>
    request<StreamOutputMutationResponse>(`/api/cameras/${encodeURIComponent(streamName)}/stream-outputs`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  probeStreamOutputs: (streamName: string) =>
    request<StreamOutputMutationResponse>(`/api/cameras/${encodeURIComponent(streamName)}/stream-outputs/probe`, {
      method: "POST",
    }),
  reapplyStreamOutputs: (streamName: string, expectedDesiredRevision: number) =>
    request<StreamOutputMutationResponse>(`/api/cameras/${encodeURIComponent(streamName)}/stream-outputs/reapply`, {
      method: "POST",
      body: JSON.stringify({ expectedDesiredRevision }),
    }),
  probeAllStreamOutputs: () =>
    request<BulkStreamOutputProbeResponse>("/api/cameras/stream-outputs/probe", { method: "POST" }),
  cameraProfiles: () => request<CameraProfileTemplate[]>("/api/camera-profiles"),
  cameraProfile: (id: number) => request<CameraProfileTemplate>(`/api/camera-profiles/${encodeURIComponent(String(id))}`),
  createCameraProfile: (profile: CameraProfileTemplateInput) =>
    request<CameraProfileTemplate>("/api/camera-profiles", { method: "POST", body: JSON.stringify(profile) }),
  updateCameraProfile: (id: number, profile: CameraProfileTemplateInput) =>
    request<CameraProfileTemplate>(`/api/camera-profiles/${encodeURIComponent(String(id))}`, {
      method: "PUT",
      body: JSON.stringify(profile),
    }),
  deleteCameraProfile: (id: number) =>
    request<{ readonly ok: boolean; readonly id: number }>(`/api/camera-profiles/${encodeURIComponent(String(id))}`, {
      method: "DELETE",
    }),
} as const;
