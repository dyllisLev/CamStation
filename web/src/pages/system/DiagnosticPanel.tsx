import { FileArchive, Loader2, RefreshCw, Trash2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import {
  useCreateDiagnostic,
  useDeleteDiagnosticArtifact,
  useDeleteDiagnosticHistory,
  useDiagnosticArtifacts,
  useSystemJobs,
} from "../../app/streamsViewersSystemQueries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { errorMessage, formatBytes, formatDate, jobBadgeState } from "./systemFormat";

type ConfirmState = {
  readonly action: "artifact" | "history" | "artifactId";
  readonly id?: number;
} | null;

export function DiagnosticPanel() {
  const createDiagnostic = useCreateDiagnostic();
  const artifacts = useDiagnosticArtifacts();
  const jobs = useSystemJobs();
  const deleteArtifact = useDeleteDiagnosticArtifact();
  const deleteHistory = useDeleteDiagnosticHistory();
  const [reason, setReason] = useState("operator diagnostic");
  const [artifactId, setArtifactId] = useState("");
  const [confirm, setConfirm] = useState<ConfirmState>(null);
  const diagnosticJobs = (jobs.data ?? []).filter((job) => job.kind.includes("diagnostic"));

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    createDiagnostic.mutate(reason);
  }

  function removeArtifact(id: number, action: "artifact" | "artifactId") {
    if (confirm?.action !== action || confirm.id !== id) {
      setConfirm({ action, id });
      return;
    }
    deleteArtifact.mutate(id, { onSettled: () => setConfirm(null) });
  }

  function removeHistory() {
    if (confirm?.action !== "history") {
      setConfirm({ action: "history" });
      return;
    }
    deleteHistory.mutate(undefined, {
      onSettled: () => setConfirm(null),
      onSuccess: () => void jobs.refetch(),
    });
  }

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">진단 번들</h2>
          <div className="mt-1 text-xs text-slate-500">메타데이터와 아티팩트만 표시합니다.</div>
        </div>
        <Button size="sm" type="button" variant="secondary" onClick={() => void Promise.all([artifacts.refetch(), jobs.refetch()])}>
          <RefreshCw size={15} />
          새로고침
        </Button>
      </PanelHeader>
      <PanelBody className="space-y-4">
        <form className="grid gap-3 md:grid-cols-[1fr_auto]" onSubmit={submit}>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">사유</span>
            <input className="new-form-control" value={reason} onChange={(event) => setReason(event.target.value)} />
          </label>
          <Button className="self-end" disabled={createDiagnostic.isPending} type="submit" variant="primary">
            {createDiagnostic.isPending ? <Loader2 className="animate-spin" size={16} /> : <FileArchive size={16} />}
            진단 생성
          </Button>
        </form>

        <div className="new-table-wrap">
          <table className="new-table">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">아티팩트</th>
                <th className="px-3 py-2 font-medium">크기</th>
                <th className="px-3 py-2 font-medium">SHA256</th>
                <th className="px-3 py-2 font-medium">생성</th>
                <th className="px-3 py-2 font-medium">삭제</th>
              </tr>
            </thead>
            <tbody>
              {(artifacts.data ?? []).map((artifact) => (
                <tr key={artifact.id}>
                  <td className="px-3 py-3">
                    <div className="font-semibold text-slate-100">{artifact.name}</div>
                    <div className="mt-1 font-mono text-xs text-slate-500">job #{artifact.jobId}</div>
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-400">{formatBytes(artifact.sizeBytes)}</td>
                  <td className="max-w-60 truncate px-3 py-3 font-mono text-xs text-slate-500">{artifact.sha256}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-500">{formatDate(artifact.createdAt)}</td>
                  <td className="px-3 py-3">
                    <Button disabled={deleteArtifact.isPending} size="sm" type="button" variant="danger" onClick={() => removeArtifact(artifact.id, "artifact")}>
                      <Trash2 size={14} />
                      {confirm?.action === "artifact" && confirm.id === artifact.id ? "삭제 확인" : "삭제"}
                    </Button>
                  </td>
                </tr>
              ))}
              {!artifacts.data?.length && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={5}>
                    진단 아티팩트가 없습니다.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <div className="grid gap-3 md:grid-cols-[1fr_auto_auto]">
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">아티팩트 ID 삭제</span>
            <input className="new-form-control" inputMode="numeric" value={artifactId} onChange={(event) => setArtifactId(event.target.value)} />
          </label>
          <Button
            className="self-end"
            disabled={deleteArtifact.isPending || artifactId === ""}
            type="button"
            variant="danger"
            onClick={() => removeArtifact(Number(artifactId), "artifactId")}
          >
            {confirm?.action === "artifactId" && confirm.id === Number(artifactId) ? "ID 삭제 확인" : "ID 삭제"}
          </Button>
          <Button className="self-end" disabled={deleteHistory.isPending} type="button" variant="danger" onClick={removeHistory}>
            {confirm?.action === "history" ? "이력 삭제 확인" : "진단 이력 삭제"}
          </Button>
        </div>

        <div className="grid gap-2">
          {diagnosticJobs.slice(0, 5).map((job) => (
            <div key={job.id} className="flex flex-wrap items-center justify-between gap-2 rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-2 text-xs">
              <span className="font-mono text-slate-400">#{job.id} · {job.kind}</span>
              <span className="text-slate-500">{formatDate(job.updatedAt)}</span>
              <Badge value={jobBadgeState(job.state)} />
            </div>
          ))}
          {!diagnosticJobs.length && <div className="text-xs text-slate-500">진단 작업 이력이 없습니다.</div>}
        </div>

        {createDiagnostic.error && <div className="text-xs text-red-300">{errorMessage(createDiagnostic.error)}</div>}
        {artifacts.error && <div className="text-xs text-red-300">{errorMessage(artifacts.error)}</div>}
        {jobs.error && <div className="text-xs text-red-300">{errorMessage(jobs.error)}</div>}
        {deleteArtifact.error && <div className="text-xs text-red-300">{errorMessage(deleteArtifact.error)}</div>}
        {deleteHistory.error && <div className="text-xs text-red-300">{errorMessage(deleteHistory.error)}</div>}
        {createDiagnostic.isSuccess && <div className="text-xs text-emerald-300">진단 작업이 생성되었습니다.</div>}
      </PanelBody>
    </Panel>
  );
}
