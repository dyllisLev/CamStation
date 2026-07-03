import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type CameraPreviewRequest, type CameraScanRequest, type CreateCamera, type LayoutProfile, type UpdateCamera } from "./api";

export * from "./backupQueries";
export * from "./eventsIncidentsQueries";
export * from "./recordingsQueries";
export * from "./settingsJobsQueries";
export * from "./streamsViewersSystemQueries";

export function useHealth() {
  return useQuery({ queryKey: ["health"], queryFn: api.health, refetchInterval: 15000 });
}

export function useCameras() {
  return useQuery({ queryKey: ["cameras"], queryFn: api.cameras, refetchInterval: 10000 });
}

export function useLayouts() {
  return useQuery({ queryKey: ["layouts"], queryFn: api.layouts });
}

export function useTimeline(camera: string, date: string) {
  return useQuery({
    queryKey: ["timeline", camera, date],
    queryFn: () => api.timeline(camera, date),
    enabled: Boolean(camera && date),
    refetchInterval: 30000,
  });
}

export function useEvents() {
  return useQuery({ queryKey: ["events"], queryFn: api.events, refetchInterval: 7000 });
}

export function useRecorderStatus() {
  return useQuery({
    queryKey: ["recorder-status"],
    queryFn: api.recorderStatus,
    refetchInterval: 5000,
  });
}

export function useRecordingStorage() {
  return useQuery({
    queryKey: ["recording-storage"],
    queryFn: api.recordingStorage,
    refetchInterval: 5000,
  });
}

export function useCleanupRecordings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (maxBytes: number) => api.cleanupRecordings(maxBytes),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["recording-storage"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
        queryClient.invalidateQueries({ queryKey: ["timeline"] }),
      ]);
    },
  });
}

export function useCreateLayout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (layout: Pick<LayoutProfile, "name" | "data" | "timeline_collapsed" | "grid_cols" | "grid_rows">) =>
      api.createLayout(layout),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["layouts"] });
    },
  });
}

export function useUpdateLayout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      layout,
    }: {
      id: string;
      layout: Partial<Pick<LayoutProfile, "name" | "data" | "timeline_collapsed" | "grid_cols" | "grid_rows">>;
    }) => api.updateLayout(id, layout),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["layouts"] });
    },
  });
}

export function useStreamStatus() {
  return useQuery({
    queryKey: ["stream-status"],
    queryFn: api.streamStatus,
    refetchInterval: 5000,
  });
}

export function useCreateCamera() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (camera: CreateCamera) => api.createCamera(camera),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["cameras"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
        queryClient.invalidateQueries({ queryKey: ["stream-status"] }),
        queryClient.invalidateQueries({ queryKey: ["recorder-status"] }),
      ]);
    },
  });
}

export function useUpdateCamera() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ streamName, camera }: { streamName: string; camera: UpdateCamera }) => api.updateCamera(streamName, camera),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["cameras"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
        queryClient.invalidateQueries({ queryKey: ["stream-status"] }),
        queryClient.invalidateQueries({ queryKey: ["recorder-status"] }),
      ]);
    },
  });
}

export function useDeleteCamera() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (streamName: string) => api.deleteCamera(streamName),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["cameras"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
        queryClient.invalidateQueries({ queryKey: ["stream-status"] }),
        queryClient.invalidateQueries({ queryKey: ["recorder-status"] }),
        queryClient.invalidateQueries({ queryKey: ["timeline"] }),
      ]);
    },
  });
}

export function useScanCamera() {
  return useMutation({
    mutationFn: (camera: CameraScanRequest) => api.scanCamera(camera),
  });
}

export function usePreviewCamera() {
  return useMutation({
    mutationFn: (camera: CameraPreviewRequest) => api.previewCamera(camera),
  });
}

export function useScanRegisteredCamera() {
  return useMutation({
    mutationFn: ({ streamName, camera }: { streamName: string; camera: CameraScanRequest }) =>
      api.scanRegisteredCamera(streamName, camera),
  });
}

export function usePreviewRegisteredCamera() {
  return useMutation({
    mutationFn: ({ streamName, camera }: { streamName: string; camera: CameraPreviewRequest }) =>
      api.previewRegisteredCamera(streamName, camera),
  });
}

export function useRestartStreams() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.restartStreams,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["stream-status"] }),
        queryClient.invalidateQueries({ queryKey: ["streams", "status"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}
