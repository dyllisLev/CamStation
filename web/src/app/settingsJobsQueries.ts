import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type JobCancelInput, type JobCreate, type JobFailInput, type JobResultInput, type SettingsUpdate } from "./api";

export const settingsKeys = {
  all: ["settings"] as const,
};

export const jobKeys = {
  all: ["jobs"] as const,
  detail: (id: number) => ["jobs", id] as const,
};

export function useSettings() {
  return useQuery({ queryKey: settingsKeys.all, queryFn: api.settings });
}

export function useUpdateSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (settings: SettingsUpdate) => api.updateSettings(settings),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: settingsKeys.all }),
        queryClient.invalidateQueries({ queryKey: ["backup"] }),
      ]);
    },
  });
}

export function useResetSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.resetSettings,
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: settingsKeys.all }),
        queryClient.invalidateQueries({ queryKey: ["backup"] }),
      ]);
    },
  });
}

export function useTestAlert() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: api.testAlert,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["events"] });
    },
  });
}

export function useJobs() {
  return useQuery({ queryKey: jobKeys.all, queryFn: api.jobs, refetchInterval: 5000 });
}

export function useJob(id: number) {
  return useQuery({ queryKey: jobKeys.detail(id), queryFn: () => api.job(id), enabled: id > 0 });
}

export function useCreateJob() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (job: JobCreate) => api.createJob(job),
    onSuccess: async (job) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: jobKeys.all }),
        queryClient.invalidateQueries({ queryKey: jobKeys.detail(job.id) }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}

export function useStartJob() {
  return useJobMutation((id: number) => api.startJob(id));
}

export function useSucceedJob() {
  return useJobMutation(({ id, input }: { readonly id: number; readonly input: JobResultInput }) =>
    api.succeedJob(id, input),
  );
}

export function useFailJob() {
  return useJobMutation(({ id, input }: { readonly id: number; readonly input: JobFailInput }) => api.failJob(id, input));
}

export function useCancelJob() {
  return useJobMutation(({ id, input }: { readonly id: number; readonly input: JobCancelInput }) =>
    api.cancelJob(id, input),
  );
}

export function useDeleteJob() {
  return useJobMutation((id: number) => api.deleteJob(id));
}

function useJobMutation<TInput>(mutationFn: (input: TInput) => ReturnType<typeof api.job>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async (job) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: jobKeys.all }),
        queryClient.invalidateQueries({ queryKey: jobKeys.detail(job.id) }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}
