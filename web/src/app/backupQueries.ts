import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type BackupSettings, type BackupStartInput } from "./api";

export const backupKeys = {
  all: ["backup"] as const,
  config: ["backup", "config"] as const,
  status: ["backup", "status"] as const,
  jobs: ["backup", "jobs"] as const,
  job: (id: number) => ["backup", "jobs", id] as const,
};

export function useBackupConfig() {
  return useQuery({ queryKey: backupKeys.config, queryFn: api.backupConfig });
}

export function useUpdateBackupConfig() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (config: BackupSettings) => api.updateBackupConfig(config),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: backupKeys.all }),
        queryClient.invalidateQueries({ queryKey: ["settings"] }),
      ]);
    },
  });
}

export function useBackupStatus() {
  return useQuery({ queryKey: backupKeys.status, queryFn: api.backupStatus, refetchInterval: 5000 });
}

export function useBackupJobs() {
  return useQuery({ queryKey: backupKeys.jobs, queryFn: api.backupJobs, refetchInterval: 5000 });
}

export function useBackupJob(id: number) {
  return useQuery({ queryKey: backupKeys.job(id), queryFn: () => api.backupJob(id), enabled: id > 0 });
}

export function useStartBackup() {
  return useBackupJobMutation((input: BackupStartInput) => api.startBackup(input));
}

export function useCancelBackupJob() {
  return useBackupJobMutation((id: number) => api.cancelBackupJob(id));
}

export function useRetryBackupJob() {
  return useBackupJobMutation((id: number) => api.retryBackupJob(id));
}

export function useDeleteBackupJob() {
  return useBackupJobMutation((id: number) => api.deleteBackupJob(id));
}

function useBackupJobMutation<TInput>(mutationFn: (input: TInput) => ReturnType<typeof api.backupJob>) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: async (job) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: backupKeys.all }),
        queryClient.invalidateQueries({ queryKey: backupKeys.job(job.id) }),
        queryClient.invalidateQueries({ queryKey: ["jobs"] }),
        queryClient.invalidateQueries({ queryKey: ["events"] }),
      ]);
    },
  });
}
