import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type CreateCamera } from "./api";

export function useHealth() {
  return useQuery({ queryKey: ["health"], queryFn: api.health, refetchInterval: 15000 });
}

export function useCameras() {
  return useQuery({ queryKey: ["cameras"], queryFn: api.cameras, refetchInterval: 10000 });
}

export function useEvents() {
  return useQuery({ queryKey: ["events"], queryFn: api.events, refetchInterval: 7000 });
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

