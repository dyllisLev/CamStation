import axios from 'axios';
import type { Camera, CameraAdminApplyResult, CameraAdminCreateRequest, CameraAdminItem, CameraAdminUpdateRequest, CameraConfigStatus, CameraRebootResult, RecordingSegment, TimelineData, Settings, SystemStatus, LayoutItem, LayoutProfile, StorageStats, SystemVersion, ViewerClientStatus, ViewerCommand, ViewerHeartbeatPayload } from '../types';

const api = axios.create({ baseURL: '/api' });

export const getCameras = (): Promise<Camera[]> =>
  api.get('/cameras').then(r => r.data);

export const getCameraConfig = (): Promise<CameraConfigStatus[]> =>
  api.get('/cameras/config').then(r => r.data);

export const updateCameraEnabled = (cameraId: string, enabled: boolean): Promise<CameraConfigStatus> =>
  api.patch(`/cameras/${encodeURIComponent(cameraId)}/enabled`, { enabled }).then(r => r.data);

export const rebootCamera = (cameraId: string): Promise<CameraRebootResult> =>
  api.post(`/cameras/${encodeURIComponent(cameraId)}/reboot`).then(r => r.data);

export const getCameraAdmin = (): Promise<CameraAdminItem[]> =>
  api.get('/camera-admin').then(r => r.data);

export const createCameraAdmin = (payload: CameraAdminCreateRequest): Promise<CameraAdminItem> =>
  api.post('/camera-admin', payload).then(r => r.data);

export const updateCameraAdmin = (cameraId: string, payload: CameraAdminUpdateRequest): Promise<CameraAdminItem> =>
  api.patch(`/camera-admin/${encodeURIComponent(cameraId)}`, payload).then(r => r.data);

export const setCameraAdminEnabled = (cameraId: string, enabled: boolean): Promise<CameraAdminItem> =>
  api.post(`/camera-admin/${encodeURIComponent(cameraId)}/enabled`, { enabled }).then(r => r.data);

export const archiveCameraAdmin = (cameraId: string): Promise<CameraAdminItem> =>
  api.delete(`/camera-admin/${encodeURIComponent(cameraId)}`).then(r => r.data);

export const applyCameraAdminConfig = (): Promise<CameraAdminApplyResult> =>
  api.post('/camera-admin/apply').then(r => r.data);

export const getTimeline = (cam: string, date: string): Promise<TimelineData> =>
  api.get('/timeline', { params: { cam, date } }).then(r => r.data);

export const getSettings = (): Promise<Settings> =>
  api.get('/settings').then(r => r.data);

export const updateSettings = (s: Settings): Promise<Settings> =>
  api.post('/settings', s).then(r => r.data);

export const getStatus = (): Promise<SystemStatus> =>
  api.get('/status').then(r => r.data);

export const listRecordings = (cam: string, date: string): Promise<RecordingSegment[]> =>
  api.get(`/recordings/${encodeURIComponent(cam)}/${date}`).then(r => r.data);

export const getStorageStats = (): Promise<StorageStats> =>
  api.get('/recordings/stats').then(r => r.data);

export const getLayouts = (): Promise<LayoutProfile[]> =>
  api.get('/layouts').then(r => r.data);

export const createLayout = (req: { name: string; data: LayoutItem[]; timeline_collapsed: boolean; grid_cols?: number; grid_rows?: number | null }): Promise<LayoutProfile> =>
  api.post('/layouts', req).then(r => r.data);

export const updateLayout = (
  id: string,
  req: { name?: string; data?: LayoutItem[]; timeline_collapsed?: boolean; grid_cols?: number; grid_rows?: number | null },
): Promise<LayoutProfile> =>
  api.put(`/layouts/${id}`, req).then(r => r.data);

export const deleteLayout = (id: string): Promise<void> =>
  api.delete(`/layouts/${id}`);

export const getSystemVersion = (): Promise<SystemVersion> =>
  api.get('/system/version').then(r => r.data);

export const triggerUpdate = (): Promise<{ status: string }> =>
  api.post('/system/update').then(r => r.data);

export const getViewerVersion = (): Promise<{ version: string }> =>
  api.get('/settings/viewer-version').then(r => r.data);

export const sendViewerHeartbeat = (payload: ViewerHeartbeatPayload): Promise<ViewerClientStatus> =>
  api.post('/viewers/heartbeat', payload).then(r => r.data);

export const getPendingViewerCommand = (clientId: string): Promise<ViewerCommand | null> =>
  api.get(`/viewers/${encodeURIComponent(clientId)}/commands/pending`, { validateStatus: status => status === 200 || status === 204 })
    .then(r => r.status === 204 ? null : r.data);

export const completeViewerCommand = (
  clientId: string,
  commandId: number,
  result: { ok: boolean; message?: string; details?: Record<string, unknown> },
): Promise<ViewerCommand> =>
  api.post(`/viewers/${encodeURIComponent(clientId)}/commands/${commandId}/complete`, result).then(r => r.data);
