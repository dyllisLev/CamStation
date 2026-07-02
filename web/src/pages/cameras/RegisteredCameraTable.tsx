import type { Camera } from "../../app/api";
import { StatusDot } from "../../components/StatusDot";
import { Badge } from "../../components/ui/badge";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { formatDate, formatDurationNanos } from "../../lib/utils";
import { roleLabel } from "./model";

type RegisteredCameraTableProps = {
  cameras: Camera[];
  selectedCameraId: number | null;
  onSelectCamera: (cameraId: number) => void;
};

export function RegisteredCameraTable({ cameras, selectedCameraId, onSelectCamera }: RegisteredCameraTableProps) {
  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-slate-100">등록된 카메라</h2>
          <p className="mt-1 text-xs text-slate-500">제조사, 모델, 녹화/라이브 역할 스트림을 확인합니다.</p>
        </div>
        <Badge value={cameras.length > 0 ? "running" : "unknown"} />
      </PanelHeader>
      <PanelBody>
        <div className="new-table-wrap">
          <table className="new-table new-camera-table">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">카메라</th>
                <th className="px-3 py-2 font-medium">프로파일</th>
                <th className="px-3 py-2 font-medium">역할별 스트림</th>
                <th className="px-3 py-2 font-medium">상태</th>
                <th className="px-3 py-2 font-medium">업데이트</th>
                <th className="px-3 py-2 font-medium">선택</th>
              </tr>
            </thead>
            <tbody>
              {cameras.map((camera) => (
                <CameraRow
                  camera={camera}
                  selected={camera.id === selectedCameraId}
                  onSelect={() => onSelectCamera(camera.id)}
                  key={camera.id}
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

function CameraRow({ camera, selected, onSelect }: { camera: Camera; selected: boolean; onSelect: () => void }) {
  const streams = camera.streams ?? [];
  return (
    <tr className={selected ? "new-camera-selected-row" : undefined}>
      <td className="max-w-72 px-3 py-3" data-label="카메라">
        <div className="font-semibold text-slate-100">{camera.name}</div>
        <div className="mt-1 font-mono text-xs text-slate-500">{camera.streamName}</div>
      </td>
      <td className="px-3 py-3" data-label="프로파일">
        <div className="text-slate-300">{camera.manufacturer || "-"}</div>
        <div className="mt-1 text-xs text-slate-500">{camera.model || camera.profileAdapter || "-"}</div>
      </td>
      <td className="px-3 py-3" data-label="스트림">
        <div className="new-camera-streams">
          {streams.map((stream) => (
            <span key={stream.go2rtcStreamName}>
              {roleLabel(stream.role)} <em>{stream.go2rtcStreamName}</em>
            </span>
          ))}
          {streams.length === 0 && <span>기본 <em>{camera.streamName}</em></span>}
        </div>
      </td>
      <td className="px-3 py-3" data-label="상태">
        <span className="inline-flex items-center gap-2">
          <StatusDot status={camera.state} />
          <Badge value={camera.state} />
        </span>
        <div className="mt-1 text-xs text-slate-500">{formatDurationNanos(camera.lastProbe?.duration)}</div>
      </td>
      <td className="px-3 py-3 text-slate-500" data-label="업데이트">{formatDate(camera.updatedAt)}</td>
      <td className="px-3 py-3" data-label="선택">
        <button className="new-ghost" type="button" onClick={onSelect} aria-pressed={selected}>
          프로파일 설정
        </button>
      </td>
    </tr>
  );
}
