import axios from 'axios';
import type { Camera, TimelineData, Settings, SystemStatus } from '../types';

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

export const listRecordings = (cam: string, date: string): Promise<string[]> =>
  api.get(`/recordings/${encodeURIComponent(cam)}/${date}`).then(r => r.data);
