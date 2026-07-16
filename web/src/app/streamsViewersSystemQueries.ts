import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  api,
  type MaintenanceInput,
  type ViewerCommandInput,
  type ViewerCommandUpdate,
  type ViewerHeartbeat,
  type ViewerUpdate,
} from "./api";

export const streamKeys = {
  status: ["streams", "status"] as const,
  operation: (streamName: string) => ["streams", "operation", streamName] as const,
};

export const viewerKeys = {
  all: ["viewers"] as const,
  commands: (id: string) => ["viewers", id, "commands"] as const,
};

export const systemKeys = {
  status: ["system", "status"] as const,
  jobs: ["system", "jobs"] as const,
  artifacts: ["system", "diagnostics", "artifacts"] as const,
};

export function useRestartStream() {
  return useStreamMutation((streamName: string) => api.restartStream(streamName));
}

export function useProbeStream() {
  return useStreamMutation((streamName: string) => api.probeStream(streamName));
}

export function useRejectStreamDelete() {
  return useMutation({ mutationFn: (streamName: string) => api.rejectStreamDelete(streamName) });
}

export function useViewerHeartbeat() {
  return useViewerMutation((heartbeat: ViewerHeartbeat) => api.viewerHeartbeat(heartbeat));
}

export function useViewers() {
  return useQuery({ queryKey: viewerKeys.all, queryFn: api.viewers, refetchInterval: 15000 });
}

export function useUpdateViewer() {
  return useViewerMutation(({ id, viewer }: { readonly id: string; readonly viewer: ViewerUpdate }) =>
    api.updateViewer(id, viewer),
  );
}

export function useDeleteViewer() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteViewer(id),
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: viewerKeys.all }),
        queryClient.invalidateQueries({ queryKey: viewerKeys.commands(result.id) }),
      ]);
    },
  });
}

export function useCreateViewerCommand() {
  return useViewerCommandMutation(({ id, command }: { readonly id: string; readonly command: ViewerCommandInput }) =>
    api.createViewerCommand(id, command),
  );
}

export function useViewerCommands(id: string) {
  return useQuery({ queryKey: viewerKeys.commands(id), queryFn: () => api.viewerCommands(id), enabled: id !== "" });
}

export function useUpdateViewerCommand() {
  return useViewerCommandMutation(
    ({ id, commandID, command }: { readonly id: string; readonly commandID: number; readonly command: ViewerCommandUpdate }) =>
      api.updateViewerCommand(id, commandID, command),
  );
}

export function useCancelViewerCommand() {
  return useViewerCommandMutation(
    ({ id, commandID, reason }: { readonly id: string; readonly commandID: number; readonly reason: string }) =>
      api.cancelViewerCommand(id, commandID, reason),
  );
}

export function useDeleteViewerCommand() {
  return useViewerCommandMutation(({ id, commandID }: { readonly id: string; readonly commandID: number }) =>
    api.deleteViewerCommand(id, commandID),
  );
}

export function useSystemStatus() {
  return useQuery({ queryKey: systemKeys.status, queryFn: api.systemStatus, refetchInterval: 15000 });
}

export function useCreateDiagnostic() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (reason: string) => api.createDiagnostic(reason),
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: systemKeys.jobs }),
        queryClient.invalidateQueries({ queryKey: systemKeys.artifacts }),
        queryClient.invalidateQueries({ queryKey: ["jobs", result.job.id] }),
      ]);
    },
  });
}

export function useSystemJobs() {
  return useQuery({ queryKey: systemKeys.jobs, queryFn: api.systemJobs, refetchInterval: 5000 });
}

export function useCreateMaintenance() {
  return useSystemJobMutation((input: MaintenanceInput) => api.createMaintenance(input));
}

export function useCancelSystemJob() {
  return useSystemJobMutation((id: number) => api.cancelSystemJob(id));
}

export function useDiagnosticArtifacts() {
  return useQuery({ queryKey: systemKeys.artifacts, queryFn: api.diagnosticArtifacts });
}

export function useDeleteDiagnosticArtifact() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteDiagnosticArtifact(id),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: systemKeys.artifacts });
    },
  });
}

export function useDeleteDiagnosticHistory() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.deleteDiagnosticHistory,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: systemKeys.artifacts });
    },
  });
}

function useStreamMutation<TInput>(mutationFn: (input: TInput) => ReturnType<typeof api.restartStream>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async (result) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: streamKeys.status }),
        queryClient.invalidateQueries({ queryKey: ["stream-status"] }),
        queryClient.invalidateQueries({ queryKey: streamKeys.operation(result.streamName) }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}

function useViewerMutation<TInput, TOutput>(mutationFn: (input: TInput) => Promise<TOutput>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: viewerKeys.all }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}

function useViewerCommandMutation<TInput>(mutationFn: (input: TInput) => ReturnType<typeof api.createViewerCommand>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async (command) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: viewerKeys.commands(command.viewerId) }),
        queryClient.invalidateQueries({ queryKey: viewerKeys.all }),
      ]);
    },
  });
}

function useSystemJobMutation<TInput>(mutationFn: (input: TInput) => ReturnType<typeof api.cancelSystemJob>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async (job) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: systemKeys.jobs }),
        queryClient.invalidateQueries({ queryKey: ["jobs", job.id] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
        queryClient.invalidateQueries({ queryKey: ["recordings"] }),
      ]);
    },
  });
}
