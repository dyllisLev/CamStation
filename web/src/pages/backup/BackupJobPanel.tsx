import { Loader2, Play, Square } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useBackupStatus, useCancelBackupJob, useStartBackup } from "../../app/backupQueries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { errorMessage, formatDate, formatDuration, jobBadgeState } from "./backupFormat";

export function BackupJobPanel() {
  const status = useBackupStatus();
  const startBackup = useStartBackup();
  const cancelJob = useCancelBackupJob();
  const [prefix, setPrefix] = useState("");
  const [timeoutSeconds, setTimeoutSeconds] = useState("300");
  const [confirmStart, setConfirmStart] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const activeJob = status.data?.activeJob;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!confirmStart) {
      setConfirmStart(true);
      return;
    }
    startBackup.mutate(
      { prefix: prefix.trim() || undefined, timeoutSeconds: Number(timeoutSeconds) },
      { onSuccess: () => setConfirmStart(false) },
    );
  }

  function cancelActiveJob() {
    if (!activeJob) {
      return;
    }
    if (!confirmCancel) {
      setConfirmCancel(true);
      return;
    }
    cancelJob.mutate(activeJob.id, { onSettled: () => setConfirmCancel(false) });
  }

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">백업 작업</h2>
          <div className="mt-1 text-xs text-slate-500">원격 복사 시작, 실행 상태, 취소를 관리합니다.</div>
        </div>
        <Badge value={jobBadgeState(activeJob?.state)} />
      </PanelHeader>
      <PanelBody className="space-y-4">
        <div className="grid gap-3 md:grid-cols-4">
          <Metric label="대상" value={status.data?.config.target || "미설정"} />
          <Metric label="활성 작업" value={activeJob ? `#${activeJob.id}` : "없음"} />
          <Metric label="최근 이력" value={`${status.data?.history.length ?? 0}건`} />
          <Metric label="다음 스케줄" value={formatDate(status.data?.schedule?.nextRunAt)} />
        </div>

        {activeJob ? (
          <div className="new-feature-card grid gap-3 md:grid-cols-[1fr_auto]">
            <div className="grid gap-2 text-xs text-slate-400 sm:grid-cols-2">
              <span>상태: {activeJob.state}</span>
              <span>경과: {formatDuration(activeJob)}</span>
              <span>시작: {formatDate(activeJob.startedAt)}</span>
              <span>갱신: {formatDate(activeJob.updatedAt)}</span>
            </div>
            <Button disabled={cancelJob.isPending} type="button" variant="danger" onClick={cancelActiveJob}>
              {cancelJob.isPending ? <Loader2 className="animate-spin" size={16} /> : <Square size={16} />}
              {confirmCancel ? "취소 확인" : "작업 취소"}
            </Button>
          </div>
        ) : (
          <div className="rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-3 text-sm text-slate-500">
            실행 중인 백업 작업이 없습니다.
          </div>
        )}

        <form className="grid gap-3 lg:grid-cols-[1fr_9rem_auto]" onSubmit={submit}>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">원격 하위 prefix(선택)</span>
            <input
              className="new-form-control font-mono"
              placeholder="비우면 대상 바로 아래에 카메라/날짜/영상으로 백업"
              value={prefix}
              onChange={(event) => {
                setConfirmStart(false);
                setPrefix(event.target.value);
              }}
            />
          </label>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">제한초</span>
            <input
              className="new-form-control"
              inputMode="numeric"
              min="1"
              type="number"
              value={timeoutSeconds}
              onChange={(event) => {
                setConfirmStart(false);
                setTimeoutSeconds(event.target.value);
              }}
            />
          </label>
          <Button className="self-end" disabled={startBackup.isPending} type="submit" variant="primary">
            {startBackup.isPending ? <Loader2 className="animate-spin" size={16} /> : <Play size={16} />}
            {confirmStart ? "시작 확인" : "백업 시작"}
          </Button>
        </form>

        {status.error && <div className="text-xs text-red-300">{errorMessage(status.error)}</div>}
        {startBackup.error && <div className="text-xs text-red-300">{errorMessage(startBackup.error)}</div>}
        {cancelJob.error && <div className="text-xs text-red-300">{errorMessage(cancelJob.error)}</div>}
        {startBackup.isSuccess && <div className="text-xs text-emerald-300">백업 작업이 등록되었습니다.</div>}
      </PanelBody>
    </Panel>
  );
}

function Metric({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="new-feature-card">
      <div className="text-xs text-slate-500">{label}</div>
      <div className="mt-1 truncate text-sm font-semibold text-slate-100">{value}</div>
    </div>
  );
}
