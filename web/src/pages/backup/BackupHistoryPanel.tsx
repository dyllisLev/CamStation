import { Loader2, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { useState } from "react";
import type { Job } from "../../app/api";
import { useBackupJob, useBackupJobs, useCancelBackupJob, useDeleteBackupJob, useRetryBackupJob } from "../../app/backupQueries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { errorMessage, formatDate, formatDuration, jobBadgeState } from "./backupFormat";

type ConfirmAction = "cancel" | "retry" | "delete";

type ConfirmState = {
  readonly action: ConfirmAction;
  readonly id: number;
} | null;

export function BackupHistoryPanel() {
  const jobs = useBackupJobs();
  const [selectedId, setSelectedId] = useState(0);
  const detail = useBackupJob(selectedId);
  const cancelJob = useCancelBackupJob();
  const retryJob = useRetryBackupJob();
  const deleteJob = useDeleteBackupJob();
  const [confirm, setConfirm] = useState<ConfirmState>(null);

  function runAction(action: ConfirmAction, id: number) {
    if (confirm?.action !== action || confirm.id !== id) {
      setConfirm({ action, id });
      return;
    }
    const callbacks = { onSettled: () => setConfirm(null) };
    switch (action) {
      case "cancel":
        cancelJob.mutate(id, callbacks);
        return;
      case "retry":
        retryJob.mutate(id, callbacks);
        return;
      case "delete":
        deleteJob.mutate(id, callbacks);
        return;
    }
  }

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">백업 이력</h2>
          <div className="mt-1 text-xs text-slate-500">작업 상세, 재시도, 취소, 로컬 이력 삭제를 처리합니다.</div>
        </div>
        <Button size="sm" type="button" variant="secondary" onClick={() => void jobs.refetch()}>
          <RefreshCw size={15} />
          새로고침
        </Button>
      </PanelHeader>
      <PanelBody className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <div className="new-table-wrap">
          <table className="new-table">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">ID</th>
                <th className="px-3 py-2 font-medium">종류</th>
                <th className="px-3 py-2 font-medium">상태</th>
                <th className="px-3 py-2 font-medium">경과</th>
                <th className="px-3 py-2 font-medium">갱신</th>
                <th className="px-3 py-2 font-medium">작업</th>
              </tr>
            </thead>
            <tbody>
              {(jobs.data?.jobs ?? []).map((job) => (
                <HistoryRow
                  key={job.id}
                  confirm={confirm}
                  job={job}
                  pending={cancelJob.isPending || retryJob.isPending || deleteJob.isPending}
                  selectedId={selectedId}
                  onAction={runAction}
                  onSelect={setSelectedId}
                />
              ))}
              {!jobs.data?.jobs.length && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={6}>
                    백업 작업 이력이 없습니다.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
        <JobDetail job={detail.data} loading={detail.isLoading} />
        {jobs.error && <div className="text-xs text-red-300 xl:col-span-2">{errorMessage(jobs.error)}</div>}
        {cancelJob.error && <div className="text-xs text-red-300 xl:col-span-2">{errorMessage(cancelJob.error)}</div>}
        {retryJob.error && <div className="text-xs text-red-300 xl:col-span-2">{errorMessage(retryJob.error)}</div>}
        {deleteJob.error && <div className="text-xs text-red-300 xl:col-span-2">{errorMessage(deleteJob.error)}</div>}
      </PanelBody>
    </Panel>
  );
}

function HistoryRow({
  confirm,
  job,
  pending,
  selectedId,
  onAction,
  onSelect,
}: {
  readonly confirm: ConfirmState;
  readonly job: Job;
  readonly pending: boolean;
  readonly selectedId: number;
  readonly onAction: (action: ConfirmAction, id: number) => void;
  readonly onSelect: (id: number) => void;
}) {
  return (
    <tr className={selectedId === job.id ? "bg-cyan-400/5" : undefined}>
      <td className="whitespace-nowrap px-3 py-3 font-mono text-xs text-slate-300">#{job.id}</td>
      <td className="whitespace-nowrap px-3 py-3 text-slate-300">{job.kind}</td>
      <td className="px-3 py-3">
        <Badge value={jobBadgeState(job.state)} />
      </td>
      <td className="whitespace-nowrap px-3 py-3 text-slate-400">{formatDuration(job)}</td>
      <td className="whitespace-nowrap px-3 py-3 text-slate-500">{formatDate(job.updatedAt)}</td>
      <td className="px-3 py-3">
        <div className="flex flex-wrap gap-2">
          <Button size="sm" type="button" variant="secondary" onClick={() => onSelect(job.id)}>
            상세
          </Button>
          <Button disabled={pending} size="sm" type="button" variant="secondary" onClick={() => onAction("retry", job.id)}>
            <RotateCcw size={14} />
            {confirm?.action === "retry" && confirm.id === job.id ? "확인" : "재시도"}
          </Button>
          <Button disabled={pending} size="sm" type="button" variant="danger" onClick={() => onAction("cancel", job.id)}>
            {confirm?.action === "cancel" && confirm.id === job.id ? "취소 확인" : "취소"}
          </Button>
          <Button disabled={pending} size="sm" type="button" variant="danger" onClick={() => onAction("delete", job.id)}>
            <Trash2 size={14} />
            {confirm?.action === "delete" && confirm.id === job.id ? "삭제 확인" : "이력 삭제"}
          </Button>
        </div>
      </td>
    </tr>
  );
}

function JobDetail({ job, loading }: { readonly job?: Job; readonly loading: boolean }) {
  if (loading) {
    return <div className="new-feature-card flex items-center gap-2 text-sm text-slate-500"><Loader2 className="animate-spin" size={16} />상세를 불러오는 중입니다.</div>;
  }
  if (!job) {
    return <div className="new-feature-card text-sm text-slate-500">이력 행을 선택하면 이벤트 상세가 표시됩니다.</div>;
  }
  return (
    <div className="new-feature-card space-y-3">
      <div className="flex items-center justify-between gap-3">
        <div className="font-mono text-xs text-slate-400">작업 #{job.id}</div>
        <Badge value={jobBadgeState(job.state)} />
      </div>
      <div className="grid gap-2 text-xs text-slate-400">
        <span>생성: {formatDate(job.createdAt)}</span>
        <span>완료: {formatDate(job.completedAt)}</span>
        {job.error && <span className="text-red-300">오류: {job.error}</span>}
      </div>
      <div className="space-y-2">
        <div className="text-xs font-medium text-slate-400">이벤트</div>
        {(job.events ?? []).map((event) => (
          <div key={event.id} className="rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-2 text-xs text-slate-400">
            <div className="font-mono text-slate-500">{formatDate(event.createdAt)} · {event.type}</div>
            <div className="mt-1 text-slate-300">{event.message}</div>
          </div>
        ))}
        {!job.events?.length && <div className="text-xs text-slate-500">이벤트가 없습니다.</div>}
      </div>
    </div>
  );
}
