import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  type EventExportQuery,
  type EventPruneInput,
  type EventQuery,
  type IncidentInput,
  type IncidentQuery,
  type IncidentSnoozeInput,
} from "./api";

export const eventKeys = {
  all: ["events"] as const,
  page: (query: EventQuery = {}) => ["events", "page", query] as const,
};

export const incidentKeys = {
  all: ["incidents"] as const,
  list: (query: IncidentQuery = {}) => ["incidents", "list", query] as const,
  detail: (id: number) => ["incidents", id] as const,
};

export function useEventPage(query: EventQuery = {}) {
  return useQuery({ queryKey: eventKeys.page(query), queryFn: () => api.queryEvents(query), refetchInterval: 7000 });
}

export function useExportEvents() {
  return useMutation({ mutationFn: (query: EventExportQuery = {}) => api.exportEvents(query) });
}

export function usePruneEvents() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: EventPruneInput) => api.pruneEvents(input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: eventKeys.all });
    },
  });
}

export function useIncidents(query: IncidentQuery = {}) {
  return useQuery({ queryKey: incidentKeys.list(query), queryFn: () => api.incidents(query), refetchInterval: 7000 });
}

export function useIncident(id: number) {
  return useQuery({ queryKey: incidentKeys.detail(id), queryFn: () => api.incident(id), enabled: id > 0 });
}

export function useCreateIncident() {
  return useIncidentMutation((incident: IncidentInput) => api.createIncident(incident));
}

export function useUpdateIncident() {
  return useIncidentMutation(({ id, incident }: { readonly id: number; readonly incident: Partial<IncidentInput> }) =>
    api.updateIncident(id, incident),
  );
}

export function useAcknowledgeIncident() {
  return useIncidentMutation((id: number) => api.acknowledgeIncident(id));
}

export function useSnoozeIncident() {
  return useIncidentMutation(({ id, input }: { readonly id: number; readonly input: IncidentSnoozeInput }) =>
    api.snoozeIncident(id, input),
  );
}

export function useResolveIncident() {
  return useIncidentMutation((id: number) => api.resolveIncident(id));
}

export function useDeleteIncident() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteIncident(id),
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: incidentKeys.all }),
        queryClient.invalidateQueries({ queryKey: incidentKeys.detail(result.incident.id) }),
        queryClient.invalidateQueries({ queryKey: eventKeys.all }),
      ]);
    },
  });
}

function useIncidentMutation<TInput>(mutationFn: (input: TInput) => ReturnType<typeof api.incident>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async (incident) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: incidentKeys.all }),
        queryClient.invalidateQueries({ queryKey: incidentKeys.detail(incident.id) }),
        queryClient.invalidateQueries({ queryKey: eventKeys.all }),
      ]);
    },
  });
}
