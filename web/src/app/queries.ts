import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type CreateCamera, type LayoutProfile } from "./api";

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
      ]);
    },
  });
}

export function useRestartStreams() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.restartStreams,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["stream-status"] });
    },
  });
}
