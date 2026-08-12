import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CAMERA_POLICY_INVALIDATION_KEYS } from "../pages/cameras/streamOutputPolicyModel";
import { api, type UpdateStreamOutputsRequest } from "./api";

export function useUpdateCameraStreamOutputs() {
  return useCameraPolicyMutation(({ streamName, input }: { streamName: string; input: UpdateStreamOutputsRequest }) =>
    api.updateStreamOutputs(streamName, input),
  );
}

export function useProbeCameraStreamOutputs() {
  return useCameraPolicyMutation((streamName: string) => api.probeStreamOutputs(streamName));
}

export function useReapplyCameraStreamOutputs() {
  return useCameraPolicyMutation(({ streamName, expectedDesiredRevision }: { streamName: string; expectedDesiredRevision: number }) =>
    api.reapplyStreamOutputs(streamName, expectedDesiredRevision),
  );
}

export function useProbeAllCameraStreamOutputs() {
  return useCameraPolicyMutation(() => api.probeAllStreamOutputs());
}

function useCameraPolicyMutation<TInput, TResult>(mutationFn: (input: TInput) => Promise<TResult>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await Promise.all(CAMERA_POLICY_INVALIDATION_KEYS.map((queryKey) => queryClient.invalidateQueries({ queryKey })));
    },
  });
}
