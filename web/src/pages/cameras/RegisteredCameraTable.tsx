import type { Camera } from "../../app/api";
import { StatusDot } from "../../components/StatusDot";
import { Badge } from "../../components/ui/badge";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { formatDate, formatDurationNanos } from "../../lib/utils";
import { roleLabel } from "./model";

type RegisteredCameraTableProps = {
  cameras: Camera[];
  selectedStreamName: string | null;
  onSelectCamera: (streamName: string) => void;
  onSetEnabled: (streamName: string, enabled: boolean) => void;
  activationPending: boolean;
  activationPendingStreamName: string | null;
  activationNotice?: string;
};

export function RegisteredCameraTable({
  cameras,
  selectedStreamName,
  onSelectCamera,
  onSetEnabled,
  activationPending,
  activationPendingStreamName,
  activationNotice,
}: RegisteredCameraTableProps) {
  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-slate-100">등록된 카메라</h2>
          <p className="mt-1 text-xs text-slate-500">카메라별 송수신 사용 여부와 녹화/라이브 역할 스트림을 관리합니다.</p>
        </div>
        <Badge value={cameras.length > 0 ? "running" : "unknown"} />
      </PanelHeader>
      <PanelBody>
        {activationNotice && <div className="new-form-error mb-3">{activationNotice}</div>}
        <div className="new-table-wrap">
          <table className="new-table new-camera-table">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">카메라</th>
                <th className="px-3 py-2 font-medium">프로파일</th>
                <th className="px-3 py-2 font-medium">역할별 스트림</th>
                <th className="px-3 py-2 font-medium">상태</th>
                <th className="px-3 py-2 font-medium">업데이트</th>
                <th className="px-3 py-2 font-medium">작업</th>
              </tr>
            </thead>
            <tbody>
              {cameras.map((camera) => (
                <CameraRow
                  camera={camera}
                  selected={camera.streamName === selectedStreamName}
                  onSelect={() => onSelectCamera(camera.streamName)}
                  onSetEnabled={() => onSetEnabled(camera.streamName, !camera.enabled)}
                  activationPending={activationPending}
                  isActivationTarget={activationPendingStreamName === camera.streamName}
                  key={camera.streamName}
                />
              ))}
              {cameras.length === 0 && (
                <tr>
                  <td className="px-3 py-8 text-center text-slate-500" colSpan={6}>
                    등록된 카메라가 없습니다.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </PanelBody>
    </Panel>
  );
}

function CameraRow({
  camera,
  selected,
  onSelect,
  onSetEnabled,
  activationPending,
  isActivationTarget,
}: {
  camera: Camera;
  selected: boolean;
  onSelect: () => void;
  onSetEnabled: () => void;
  activationPending: boolean;
  isActivationTarget: boolean;
}) {
  const streams = camera.streams ?? [];
  return (
    <tr className={selected ? "new-camera-selected-row" : undefined}>
      <td className="px-3 py-3" data-label="카메라">
        <div className="new-table-value max-w-72">
          <div className="font-semibold text-slate-100">{camera.name}</div>
          <div className="mt-1 font-mono text-xs text-slate-500">{camera.streamName}</div>
        </div>
      </td>
      <td className="px-3 py-3" data-label="프로파일">
        <div className="new-table-value">
          <div className="text-slate-300">{camera.manufacturer || "-"}</div>
          <div className="mt-1 text-xs text-slate-500">{camera.model || camera.profileAdapter || "-"}</div>
        </div>
      </td>
      <td className="px-3 py-3" data-label="스트림">
        <div className="new-table-value new-camera-streams">
          {streams.map((stream) => (
            <span key={stream.sourceKey}>
              {roleLabel(stream.role)} <em>{stream.sourceKey}</em>
            </span>
          ))}
          {streams.length === 0 && <span>기본 <em>{camera.streamName}</em></span>}
        </div>
      </td>
      <td className="px-3 py-3" data-label="상태">
        <div className="new-table-value">
          <Badge
            value={camera.enabled ? "활성" : "비활성"}
            className={camera.enabled
              ? "border-emerald-500/40 bg-emerald-500/15 text-emerald-200"
              : "border-slate-700 bg-slate-900 text-slate-400"}
          />
          {camera.enabled ? (
            <span className="inline-flex items-center gap-2">
              <StatusDot status={camera.state} />
              <Badge value={camera.state} />
            </span>
          ) : (
            <span className="text-xs text-slate-500">송수신 중지</span>
          )}
          <div className="mt-1 text-xs text-slate-500">{formatDurationNanos(camera.lastProbe?.duration)}</div>
        </div>
      </td>
      <td className="px-3 py-3 text-slate-500" data-label="업데이트">
        <span className="new-table-value">{formatDate(camera.updatedAt)}</span>
      </td>
      <td className="px-3 py-3" data-label="작업">
        <div className="new-table-value flex flex-wrap gap-2">
          <button
            className="new-ghost"
            type="button"
            onClick={onSetEnabled}
            disabled={activationPending}
          >
            {isActivationTarget ? "처리 중…" : camera.enabled ? "비활성화" : "활성화"}
          </button>
          <button className="new-ghost" type="button" onClick={onSelect} aria-pressed={selected}>
            카메라 수정
          </button>
        </div>
      </td>
    </tr>
  );
}
