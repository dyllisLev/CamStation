import { backupApi } from "./backupApi";
import { cameraApi } from "./cameraApi";
import { cameraControlApi } from "./cameraControlApi";
import { eventsIncidentsApi } from "./eventsIncidentsApi";
import { recordingsApi } from "./recordingsApi";
import { settingsJobsApi } from "./settingsJobsApi";
import { streamsViewersSystemApi } from "./streamsViewersSystemApi";

export type * from "./backupApi";
export type * from "./cameraTypes";
export type * from "./eventsIncidentsApi";
export type * from "./recordingsApi";
export type * from "./settingsJobsApi";
export type * from "./streamsViewersSystemApi";

export const api = {
  ...cameraApi,
  ...cameraControlApi,
  ...eventsIncidentsApi,
  ...recordingsApi,
  ...settingsJobsApi,
  ...backupApi,
  ...streamsViewersSystemApi,
} as const;
