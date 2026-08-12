import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type RecorderControlInput, type RecordingSegmentFilter } from "./api";

export const recordingKeys = {
  storage: ["recordings", "storage"] as const,
  recorderStatus: ["recordings", "recorders"] as const,
  segments: (filter: RecordingSegmentFilter = {}) => ["recordings", "segments", filter] as const,
  segment: (id: number) => ["recordings", "segments", id] as const,
};

export function useRecordingSegments(filter: RecordingSegmentFilter = {}) {
  return useQuery({ queryKey: recordingKeys.segments(filter), queryFn: () => api.recordingSegments(filter) });
}

export function useRecordingSegment(id: number) {
  return useQuery({ queryKey: recordingKeys.segment(id), queryFn: () => api.recordingSegment(id), enabled: id > 0 });
}

export function useStartRecorder() {
  return useRecorderMutation((input: RecorderControlInput = {}) => api.startRecorder(input));
}

export function useStopRecorder() {
  return useRecorderMutation((input: RecorderControlInput = {}) => api.stopRecorder(input));
}

export function useDeleteRecordingSegment() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.deleteRecordingSegment(id),
    onSuccess: async (segment) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: recordingKeys.storage }),
        queryClient.invalidateQueries({ queryKey: ["recording-storage"] }),
        queryClient.invalidateQueries({ queryKey: ["recordings", "segments"] }),
        queryClient.invalidateQueries({ queryKey: recordingKeys.segment(segment.id) }),
        queryClient.invalidateQueries({ queryKey: ["timeline"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}

function useRecorderMutation<TInput>(mutationFn: (input: TInput) => ReturnType<typeof api.recorderStatus>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: recordingKeys.recorderStatus }),
        queryClient.invalidateQueries({ queryKey: ["recorder-status"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}
