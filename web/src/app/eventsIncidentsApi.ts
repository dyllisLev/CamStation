import { queryString, request } from "./http";
import type { JsonObject } from "./http";

export type EventLog = {
  readonly id: number;
  readonly createdAt: string;
  readonly source: string;
  readonly level: "info" | "warning" | "error" | string;
  readonly message: string;
  readonly details?: JsonObject;
};

export type EventQuery = {
  readonly level?: string;
  readonly source?: string;
  readonly search?: string;
  readonly from?: string;
  readonly to?: string;
  readonly cursor?: string;
  readonly limit?: number;
};

export type EventExportQuery = EventQuery & {
  readonly format?: "json" | "text";
};

export type EventPage = {
  readonly events: readonly EventLog[];
  readonly nextCursor?: string;
  readonly limit: number;
};

export type EventPruneInput = {
  readonly confirm: true;
  readonly before?: string;
  readonly level?: string;
  readonly source?: string;
  readonly search?: string;
};

export type EventPruneResult = {
  readonly deleted: number;
};

export type Incident = {
  readonly id: number;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly source: string;
  readonly severity: "low" | "medium" | "high" | "critical" | string;
  readonly status: "open" | "acknowledged" | "snoozed" | "resolved" | string;
  readonly title: string;
  readonly description?: string;
  readonly details?: JsonObject;
  readonly acknowledgedAt?: string;
  readonly snoozedUntil?: string;
  readonly resolvedAt?: string;
};

export type IncidentInput = {
  readonly title: string;
  readonly description?: string;
  readonly severity?: string;
  readonly status?: string;
  readonly source: string;
  readonly details?: JsonObject;
};

export type IncidentQuery = {
  readonly status?: string;
  readonly severity?: string;
  readonly source?: string;
  readonly limit?: number;
};

export type IncidentsResponse = {
  readonly incidents: readonly Incident[];
};

export type IncidentDeleteResponse = {
  readonly deleted: boolean;
  readonly incident: Incident;
};

export type IncidentSnoozeInput = {
  readonly until: string;
};

export const eventsIncidentsApi = {
  events: () => request<readonly EventLog[]>("/api/events"),
  queryEvents: (query: EventQuery = {}) => request<EventPage>(`/api/events${queryString(query)}`),
  exportEvents: (query: EventExportQuery = {}) =>
    request<{ readonly events: readonly EventLog[] }>(`/api/events/export${queryString(query)}`),
  pruneEvents: (input: EventPruneInput) =>
    request<EventPruneResult>(`/api/events${queryString(input)}`, { method: "DELETE" }),
  incidents: (query: IncidentQuery = {}) => request<IncidentsResponse>(`/api/incidents${queryString(query)}`),
  createIncident: (incident: IncidentInput) =>
    request<Incident>("/api/incidents", { method: "POST", body: JSON.stringify(incident) }),
  incident: (id: number) => request<Incident>(`/api/incidents/${id}`),
  updateIncident: (id: number, incident: Partial<IncidentInput>) =>
    request<Incident>(`/api/incidents/${id}`, { method: "PATCH", body: JSON.stringify(incident) }),
  acknowledgeIncident: (id: number) => request<Incident>(`/api/incidents/${id}/ack`, { method: "POST" }),
  snoozeIncident: (id: number, input: IncidentSnoozeInput) =>
    request<Incident>(`/api/incidents/${id}/snooze`, { method: "POST", body: JSON.stringify(input) }),
  resolveIncident: (id: number) => request<Incident>(`/api/incidents/${id}/resolve`, { method: "POST" }),
  deleteIncident: (id: number) => request<IncidentDeleteResponse>(`/api/incidents/${id}`, { method: "DELETE" }),
} as const;
