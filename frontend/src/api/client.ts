import axios from 'axios';
import type { Camera, RecordingSegment, TimelineData, Settings, SystemStatus, LayoutItem, LayoutProfile, StorageStats, SystemVersion } from '../types';

const api = axios.create({ baseURL: '/api' });

export const getCameras = (): Promise<Camera[]> =>
  api.get('/cameras').then(r => r.data);

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

export const createLayout = (req: { name: string; data: LayoutItem[]; timeline_collapsed: boolean }): Promise<LayoutProfile> =>
  api.post('/layouts', req).then(r => r.data);

export const updateLayout = (
  id: string,
  req: { name?: string; data?: LayoutItem[]; timeline_collapsed?: boolean },
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
