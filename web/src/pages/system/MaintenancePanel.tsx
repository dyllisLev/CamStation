import { Loader2, Play, Square } from "lucide-react";
import { useState, type ChangeEvent, type FormEvent } from "react";
import type { MaintenanceInput } from "../../app/api";
import { useCancelSystemJob, useCreateMaintenance, useSystemJobs } from "../../app/streamsViewersSystemQueries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { errorMessage, formatDate, isMaintenanceJob, jobBadgeState } from "./systemFormat";

type MaintenanceAction = MaintenanceInput["action"];

type ConfirmCancel = {
  readonly id: number;
} | null;

export function MaintenancePanel() {
  const jobs = useSystemJobs();
  const createMaintenance = useCreateMaintenance();
  const cancelJob = useCancelSystemJob();
  const [action, setAction] = useState<MaintenanceAction>("health_check");
  const [defer, setDefer] = useState(false);
  const [maxBytes, setMaxBytes] = useState("0");
  const [confirmCreate, setConfirmCreate] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState<ConfirmCancel>(null);
  const maintenanceJobs = (jobs.data ?? []).filter(isMaintenanceJob);

  function changeAction(event: ChangeEvent<HTMLSelectElement>) {
    switch (event.target.value) {
      case "health_check":
      case "db_vacuum":
      case "recording_cleanup":
        setAction(event.target.value);
        setConfirmCreate(false);
        return;
      default:
        setAction("health_check");
        setConfirmCreate(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!confirmCreate) {
      setConfirmCreate(true);
      return;
    }
    const bytes = Number(maxBytes);
    createMaintenance.mutate(
      { action, defer, maxBytes: bytes > 0 ? bytes : undefined },
      { onSuccess: () => setConfirmCreate(false) },
    );
  }

  function cancelMaintenance(id: number) {
    if (confirmCancel?.id !== id) {
      setConfirmCancel({ id });
      return;
    }
    cancelJob.mutate(id, { onSettled: () => setConfirmCancel(null) });
  }

  return (
    <Panel>
      <PanelHeader>
        <h2 className="text-sm font-semibold">유지보수 작업</h2>
      </PanelHeader>
      <PanelBody className="space-y-4">
        <form className="space-y-3" onSubmit={submit}>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">작업</span>
            <select className="new-form-control" value={action} onChange={changeAction}>
              <option value="health_check">health_check</option>
              <option value="db_vacuum">db_vacuum</option>
              <option value="recording_cleanup">recording_cleanup</option>
            </select>
          </label>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">최대 바이트</span>
            <input
              className="new-form-control"
              inputMode="numeric"
              min="0"
              type="number"
              value={maxBytes}
              onChange={(event) => {
                setMaxBytes(event.target.value);
                setConfirmCreate(false);
              }}
            />
          </label>
          <label className="flex items-center justify-between gap-3 rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-2">
            <span className="text-xs font-medium text-slate-300">지연 실행</span>
            <input
              aria-label="지연 실행"
              checked={defer}
              className="h-4 w-4 accent-cyan-400"
              type="checkbox"
              onChange={(event) => {
                setDefer(event.target.checked);
                setConfirmCreate(false);
              }}
            />
          </label>
          <Button className="w-full" disabled={createMaintenance.isPending} type="submit" variant="primary">
            {createMaintenance.isPending ? <Loader2 className="animate-spin" size={16} /> : <Play size={16} />}
            {confirmCreate ? "작업 실행 확인" : "작업 실행"}
          </Button>
        </form>

        <div className="space-y-2">
          {maintenanceJobs.slice(0, 8).map((job) => (
            <div key={job.id} className="rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="font-mono text-xs text-slate-400">#{job.id} · {job.kind}</span>
                <Badge value={jobBadgeState(job.state)} />
              </div>
              <div className="mt-2 flex flex-wrap items-center justify-between gap-2 text-xs text-slate-500">
                <span>{formatDate(job.updatedAt)}</span>
                <Button disabled={cancelJob.isPending} size="sm" type="button" variant="danger" onClick={() => cancelMaintenance(job.id)}>
                  <Square size={14} />
                  {confirmCancel?.id === job.id ? "취소 확인" : "작업 취소"}
                </Button>
              </div>
              {job.error && <div className="mt-2 text-xs text-red-300">{job.error}</div>}
            </div>
          ))}
          {!maintenanceJobs.length && <div className="text-xs text-slate-500">유지보수 작업 이력이 없습니다.</div>}
        </div>

        {jobs.error && <div className="text-xs text-red-300">{errorMessage(jobs.error)}</div>}
        {createMaintenance.error && <div className="text-xs text-red-300">{errorMessage(createMaintenance.error)}</div>}
        {cancelJob.error && <div className="text-xs text-red-300">{errorMessage(cancelJob.error)}</div>}
        {createMaintenance.isSuccess && <div className="text-xs text-emerald-300">유지보수 작업이 등록되었습니다.</div>}
      </PanelBody>
    </Panel>
  );
}
