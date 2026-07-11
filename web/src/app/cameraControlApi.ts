import type { CameraControlCapabilities, CameraControlStatus, CameraPreset, PTZMoveVector } from "./cameraTypes";
import { managementRequest } from "./http";

const cameraPath = (streamName: string) => `/api/cameras/${encodeURIComponent(streamName)}`;

export const cameraControlApi = {
  cameraControls: (streamName: string) =>
    managementRequest<{ readonly capabilities: CameraControlCapabilities; readonly status: CameraControlStatus }>(
      `${cameraPath(streamName)}/controls`,
    ),
  refreshCameraControls: (streamName: string) =>
    managementRequest<{ readonly capabilities: CameraControlCapabilities }>(`${cameraPath(streamName)}/controls/refresh`, {
      method: "POST",
      body: "{}",
    }),
  cameraPresets: (streamName: string) =>
    managementRequest<readonly CameraPreset[]>(`${cameraPath(streamName)}/ptz/presets`),
  moveCamera: (streamName: string, move: PTZMoveVector, signal?: AbortSignal) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/move`, {
      method: "POST",
      body: JSON.stringify(move),
      signal,
    }),
  stopCamera: (streamName: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/stop`, { method: "POST", body: "{}" }),
  gotoCameraHome: (streamName: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/home/goto`, { method: "POST", body: "{}" }),
  setCameraHome: (streamName: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/home/set`, { method: "POST", body: "{}" }),
  createCameraPreset: (streamName: string, name: string) =>
    managementRequest<CameraPreset>(`${cameraPath(streamName)}/ptz/presets`, {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  gotoCameraPreset: (streamName: string, token: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/presets/goto`, {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  deleteCameraPreset: (streamName: string, token: string) =>
    managementRequest<{ readonly ok: true }>(`${cameraPath(streamName)}/ptz/presets/delete`, {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
} as const;
