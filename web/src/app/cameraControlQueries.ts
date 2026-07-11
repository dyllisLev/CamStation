import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { cameraControlApi } from "./cameraControlApi";

type StreamTarget = { readonly streamName: string };
type PresetNameTarget = StreamTarget & { readonly name: string };
type PresetTokenTarget = StreamTarget & { readonly token: string };

export function useCameraControls(streamName: string, enabled: boolean) {
  return useQuery({
    queryKey: ["camera-controls", streamName],
    queryFn: () => cameraControlApi.cameraControls(streamName),
    enabled: Boolean(streamName && enabled),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 30_000,
  });
}

export function useCameraPresets(streamName: string, enabled: boolean) {
  return useQuery({
    queryKey: ["camera-presets", streamName],
    queryFn: () => cameraControlApi.cameraPresets(streamName),
    enabled: Boolean(streamName && enabled),
    retry: false,
    refetchOnWindowFocus: false,
  });
}

export function useRefreshCameraControls() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ streamName }: StreamTarget) => cameraControlApi.refreshCameraControls(streamName),
    retry: false,
    onSuccess: async (_data, { streamName }) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["cameras"] }),
        queryClient.invalidateQueries({ queryKey: ["camera-controls", streamName] }),
      ]);
    },
  });
}

export function useGotoCameraHome() {
  return useMutation({
    mutationFn: ({ streamName }: StreamTarget) => cameraControlApi.gotoCameraHome(streamName),
    retry: false,
  });
}

export function useSetCameraHome() {
  return useMutation({
    mutationFn: ({ streamName }: StreamTarget) => cameraControlApi.setCameraHome(streamName),
    retry: false,
  });
}

export function useCreateCameraPreset() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ streamName, name }: PresetNameTarget) => cameraControlApi.createCameraPreset(streamName, name),
    retry: false,
    onSuccess: async (_data, { streamName }) =>
      queryClient.invalidateQueries({ queryKey: ["camera-presets", streamName] }),
  });
}

export function useGotoCameraPreset() {
  return useMutation({
    mutationFn: ({ streamName, token }: PresetTokenTarget) => cameraControlApi.gotoCameraPreset(streamName, token),
    retry: false,
  });
}

export function useDeleteCameraPreset() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ streamName, token }: PresetTokenTarget) => cameraControlApi.deleteCameraPreset(streamName, token),
    retry: false,
    onSuccess: async (_data, { streamName }) =>
      queryClient.invalidateQueries({ queryKey: ["camera-presets", streamName] }),
  });
}
