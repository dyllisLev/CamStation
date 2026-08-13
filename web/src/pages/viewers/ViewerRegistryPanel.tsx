import { Loader2, RefreshCw, Save, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useCameras } from "../../app/queries";
import { useDeleteViewer, useUpdateViewer, useViewers } from "../../app/streamsViewersSystemQueries";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import {
  canDeleteViewer,
  displayViewer,
  errorMessage,
  formatDate,
  viewerAgentState,
  viewerBadgeState,
  viewerControlState,
  viewerDeleteBlockedMessage,
} from "./viewerFormat";
import { viewerCameraReceptionSummary, type ViewerCameraReceptionSummary } from "./viewerCameraReception";

type Props = {
  readonly selectedViewerId: string;
  readonly onSelectViewer: (id: string) => void;
};

export function ViewerRegistryPanel({ selectedViewerId, onSelectViewer }: Props) {
  const viewers = useViewers();
  const cameras = useCameras();
  const updateViewer = useUpdateViewer();
  const deleteViewer = useDeleteViewer();
  const [label, setLabel] = useState("");
  const [note, setNote] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const selectedViewer = useMemo(
    () => viewers.data?.find((viewer) => viewer.id === selectedViewerId),
    [selectedViewerId, viewers.data],
  );
  const viewerRows = viewers.data ?? [];
  const registryLoading = viewers.isLoading || (viewers.data === undefined && !viewers.error);
  const cameraReceptionLoading = cameras.isLoading || (cameras.data === undefined && !cameras.error);
  const selectedViewerCanDelete = selectedViewer !== undefined && canDeleteViewer(selectedViewer.status);

  useEffect(() => {
    if (!selectedViewer) {
      return;
    }
    setLabel(selectedViewer.label ?? "");
    setNote(selectedViewer.note ?? "");
    setConfirmDelete(false);
  }, [selectedViewer]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedViewerId) {
      return;
    }
    updateViewer.mutate({ id: selectedViewerId, viewer: { label, note } });
  }

  function deleteSelectedViewer() {
    if (!selectedViewerId || !selectedViewerCanDelete) {
      return;
    }
    if (!confirmDelete) {
      setConfirmDelete(true);
      return;
    }
    deleteViewer.mutate(selectedViewerId, {
      onSuccess: () => {
        setConfirmDelete(false);
        onSelectViewer("");
      },
      onError: () => setConfirmDelete(false),
    });
  }

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Viewer 레지스트리</h2>
          <div className="mt-1 text-xs text-slate-500">
            {registryLoading ? "Viewer 목록 로딩 중" : `${viewerRows.length}대 등록됨`}
          </div>
        </div>
        <Button
          size="sm"
          type="button"
          variant="secondary"
          onClick={() => void Promise.all([viewers.refetch(), cameras.refetch()])}
        >
          <RefreshCw size={15} />
          새로고침
        </Button>
      </PanelHeader>
      <PanelBody className="space-y-4">
        <div className="new-table-wrap overflow-x-auto">
          <table className="new-table min-w-[1480px]">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">Viewer</th>
                <th className="px-3 py-2 font-medium">Agent</th>
                <th className="px-3 py-2 font-medium">제어</th>
                <th className="px-3 py-2 font-medium">Viewer</th>
                <th className="px-3 py-2 font-medium">Renderer</th>
                <th className="px-3 py-2 font-medium">Agent HB</th>
                <th className="px-3 py-2 font-medium">제어 성공</th>
                <th className="px-3 py-2 font-medium">카메라 수신</th>
                <th className="px-3 py-2 font-medium">영상 진행</th>
                <th className="px-3 py-2 font-medium">선택</th>
              </tr>
            </thead>
            <tbody>
              {viewerRows.map((viewer) => (
                <tr key={viewer.id} className={selectedViewerId === viewer.id ? "bg-cyan-400/5" : undefined}>
                  <td className="px-3 py-3">
                    <div className="font-semibold text-slate-100">{displayViewer(viewer)}</div>
                    <div className="mt-1 font-mono text-xs text-slate-500">{viewer.id}</div>
                    <div className="mt-1 text-xs text-slate-600">{viewer.route} · {viewer.mode}</div>
                  </td>
                  <td className="px-3 py-3"><Badge value={viewerBadgeState(viewerAgentState(viewer))} /></td>
                  <td className="px-3 py-3"><Badge value={viewerBadgeState(viewerControlState(viewer))} /></td>
                  <td className="px-3 py-3"><Badge value={viewerBadgeState(viewer.viewer?.state)} /></td>
                  <td className="px-3 py-3"><Badge value={viewerBadgeState(viewer.renderer?.state)} /></td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-500">{formatDate(viewer.lastHeartbeatAt)}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-500">{formatDate(viewer.control?.lastSuccessAt)}</td>
                  <td className="min-w-48 px-3 py-3">
                    <CameraReceptionSummary
                      loading={cameraReceptionLoading}
                      summary={viewerCameraReceptionSummary(viewer, cameras.data ?? [])}
                    />
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-500">{formatDate(viewer.renderer?.lastProgressAt)}</td>
                  <td className="px-3 py-3">
                    <Button size="sm" type="button" variant="secondary" onClick={() => onSelectViewer(viewer.id)}>
                      선택
                    </Button>
                  </td>
                </tr>
              ))}
              {registryLoading && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={10}>
                    <div className="flex items-center justify-center gap-2">
                      <Loader2 className="animate-spin text-cyan-300" size={16} />
                      Viewer 레지스트리를 불러오는 중입니다.
                    </div>
                  </td>
                </tr>
              )}
              {!registryLoading && viewerRows.length === 0 && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={10}>
                    등록된 Viewer가 없습니다. Viewer 앱을 설치하고 서버에 연결하면 자동으로 등록됩니다.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>

        <form className="grid gap-3 lg:grid-cols-[1fr_1fr_auto]" onSubmit={submit}>
          <Field label="라벨" value={label} onChange={setLabel} />
          <Field label="운영 메모" value={note} onChange={setNote} />
          <div className="flex flex-wrap items-end gap-2">
            <Button disabled={!selectedViewerId || updateViewer.isPending} type="submit" variant="primary">
              {updateViewer.isPending ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
              저장
            </Button>
            <Button disabled={!selectedViewerCanDelete || deleteViewer.isPending} type="button" variant="danger" onClick={deleteSelectedViewer}>
              <Trash2 size={16} />
              {confirmDelete ? "삭제 확인" : "오프라인 Viewer 삭제"}
            </Button>
          </div>
        </form>

        {selectedViewer && !selectedViewerCanDelete && (
          <div className="text-xs text-slate-500" role="status">{viewerDeleteBlockedMessage()}</div>
        )}

        {viewers.error && <div className="text-xs text-red-300">{errorMessage(viewers.error)}</div>}
        {cameras.error && <div className="text-xs text-red-300">카메라 수신 기준을 불러오지 못했습니다: {errorMessage(cameras.error)}</div>}
        {updateViewer.error && <div className="text-xs text-red-300">{errorMessage(updateViewer.error)}</div>}
        {deleteViewer.error && <div className="text-xs text-red-300">{errorMessage(deleteViewer.error)}</div>}
        {updateViewer.isSuccess && <div className="text-xs text-emerald-300">Viewer 정보가 저장되었습니다.</div>}
      </PanelBody>
    </Panel>
  );
}

function CameraReceptionSummary({
  loading,
  summary,
}: {
  readonly loading: boolean;
  readonly summary: ViewerCameraReceptionSummary;
}) {
  if (loading) {
    return <span className="text-xs text-slate-500">확인 중</span>;
  }
  const allReceiving = summary.totalCount > 0 && summary.receivingCount === summary.totalCount;
  const countColor = summary.totalCount === 0
    ? "text-slate-400"
    : allReceiving
      ? "text-emerald-300"
      : summary.receivingCount > 0
        ? "text-amber-300"
        : "text-red-300";

  return (
    <div>
      <div className={`font-semibold tabular-nums ${countColor}`}>
        {summary.receivingCount}/{summary.totalCount}
        <span className="ml-1.5 text-[11px] font-normal text-slate-500">수신 중</span>
      </div>
      {summary.missing.length > 0 && (
        <div className="mt-1 max-w-64 text-xs leading-5 text-red-300" title={summary.missing.map((item) => item.cameraName).join(", ")}>
          미수신: {summary.missing.map((item) => item.cameraName).join(", ")}
        </div>
      )}
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
}) {
  return (
    <label className="block space-y-2">
      <span className="text-xs font-medium text-slate-400">{label}</span>
      <input className="new-form-control" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}
