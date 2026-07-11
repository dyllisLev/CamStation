import { AlertTriangle, Camera, Database, Eye, RadioTower, Users, Video, X } from "lucide-react";
import { useState } from "react";
import type { Camera as CameraModel } from "../app/api";
import { withAppBase } from "../app/basePath";
import { StatusDot } from "../components/StatusDot";
import { useMseStream } from "../components/live/useMseStream";
import {
  useCameras,
  useEvents,
  useRecorderStatus,
  useRecordingStorage,
  useStreamStatus,
} from "../app/queries";

function ControlRoomDashboard() {
  const cameras = useCameras();
  const streams = useStreamStatus();
  const recorders = useRecorderStatus();
  const storage = useRecordingStorage();
  const events = useEvents();
  const [previewCamera, setPreviewCamera] = useState<CameraModel | null>(null);

  const cameraRows = cameras.data ?? [];
  const recorderWorkers = recorders.data?.workers ?? [];
  const recentErrors = (events.data ?? []).filter((event) => event.level === "error").length;
  const online = cameraRows.filter((camera) => camera.state === "streaming").length;
  const runningRecorders = recorderWorkers.filter((worker) => worker.state === "running").length;
  const storageBytes = (storage.data?.recordingsBytes ?? 0) + (storage.data?.tempBytes ?? 0);
  const streamRuntime = streams.data?.streams ?? {};
  const runningStreams = cameraRows.filter((camera) => streamRuntime[liveStreamName(camera)]?.state === "running").length;
  const viewerConnections = Object.values(streamRuntime).reduce((sum, stream) => sum + stream.viewerCount, 0);
  const streamManagerState = streams.data?.running ? "go2rtc running" : "go2rtc offline";

  const summary = [
    { label: "카메라", value: `${online}/${cameraRows.length}`, detail: "온라인 / 전체", icon: Camera },
    { label: "스트림 상태", value: `${runningStreams}/${cameraRows.length}`, detail: streamManagerState, icon: RadioTower },
    { label: "녹화", value: `${runningRecorders}/${recorderWorkers.length}`, detail: "실행 워커", icon: Video },
    { label: "시청 연결", value: String(viewerConnections), detail: "브라우저/뷰어 연결", icon: Users },
    {
      label: "저장공간",
      value: formatBytes(storageBytes),
      detail: storage.data?.autoCleanupEnabled ? "자동정리 켜짐" : "자동정리 꺼짐",
      icon: Database,
    },
    { label: "최근 오류", value: String(recentErrors), detail: "최근 이벤트 100개 기준", icon: AlertTriangle },
  ];

  return (
    <div className="new-control-room">
      <section className="new-control-summary" aria-label="관제실 요약">
        {summary.map((item) => (
          <div className="new-control-stat" key={item.label}>
            <div className="new-feature-icon">
              <item.icon size={17} />
            </div>
            <div>
              <div className="new-control-stat-label">{item.label}</div>
              <div className="new-control-stat-value">{item.value}</div>
              <div className="new-control-stat-detail">{item.detail}</div>
            </div>
          </div>
        ))}
      </section>

      <section className="new-control-grid">
        <div className="new-panel">
          <div className="new-panel-header">
            <h2 className="text-sm font-semibold">카메라 상태</h2>
          </div>
          <div className="new-panel-body">
            <div className="new-table-wrap">
              <table className="new-table new-control-table">
                <thead>
                  <tr>
                    <th className="px-3 py-2 font-medium">카메라</th>
                    <th className="px-3 py-2 font-medium">카메라 연결</th>
                    <th className="px-3 py-2 font-medium">스트림 상태</th>
                    <th className="px-3 py-2 font-medium">시청 연결</th>
                    <th className="px-3 py-2 font-medium">녹화</th>
                    <th className="px-3 py-2 font-medium">최근 오류</th>
                    <th className="px-3 py-2 font-medium">작업</th>
                  </tr>
                </thead>
                <tbody>
                  {cameraRows.map((camera) => {
                    const worker = recorderWorkers.find((item) => item.streamName === recordingStreamName(camera));
                    const runtime = streamRuntime[liveStreamName(camera)];
                    const streamState = runtime?.state ?? (streams.data?.running ? "idle" : "offline");
                    const viewerCount = runtime?.viewerCount;
                    const recordingState = worker?.state ?? "stopped";
                    const recentError = worker?.lastError ?? (camera.lastProbe?.reachable === false ? "probe failed" : "-");
                    return (
                      <tr key={camera.streamName}>
                        <td className="px-3 py-3">
                          <div className="font-semibold text-slate-100">{camera.name}</div>
                          <div className="mt-1 font-mono text-xs text-slate-500">{camera.streamName}</div>
                        </td>
                        <td className="px-3 py-3">
                          <span className="inline-flex items-center gap-2">
                            <StatusDot status={camera.state === "streaming" ? "running" : camera.state} />
                            {camera.state}
                          </span>
                        </td>
                        <td className="px-3 py-3">
                          <span className="inline-flex items-center gap-2">
                            <StatusDot status={streamState === "running" ? "running" : streamState} />
                            {streamState}
                          </span>
                        </td>
                        <td className="px-3 py-3">{viewerCount ?? "-"}</td>
                        <td className="px-3 py-3">
                          <span className="inline-flex items-center gap-2">
                            <StatusDot status={recordingState === "running" ? "running" : "offline"} />
                            {recordingState}
                          </span>
                        </td>
                        <td className="max-w-80 truncate px-3 py-3 text-slate-400">{recentError}</td>
                        <td className="px-3 py-3">
                          <div className="new-control-actions">
                            <button className="new-ghost" type="button" onClick={() => setPreviewCamera(camera)}>
                              <Eye size={14} />
                              보기
                            </button>
                            <a className="new-ghost" href={withAppBase("/live")}>라이브</a>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                  {cameraRows.length === 0 && (
                    <tr>
                      <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={7}>
                        등록된 카메라가 없습니다.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <aside className="new-control-ops">
          <section className="new-panel">
            <div className="new-panel-header">
              <h2 className="text-sm font-semibold">Recorder workers</h2>
            </div>
            <div className="new-panel-body space-y-2">
              {recorderWorkers.map((worker) => (
                <div className="new-control-row" key={worker.streamName}>
                  <span>{worker.streamName}</span>
                  <em>{worker.state}</em>
                </div>
              ))}
              {recorderWorkers.length === 0 && <div className="text-xs text-slate-500">실행 중인 녹화 워커가 없습니다.</div>}
            </div>
          </section>
          <section className="new-panel">
            <div className="new-panel-header">
              <h2 className="text-sm font-semibold">Stream manager</h2>
            </div>
            <div className="new-panel-body space-y-2">
              <div className="new-control-row">
                <span>go2rtc</span>
                <em>{streams.data?.running ? "running" : "offline"}</em>
              </div>
              <div className="new-control-row">
                <span>Streams</span>
                <em>{Object.keys(streamRuntime).length}</em>
              </div>
            </div>
          </section>
          <section className="new-panel">
            <div className="new-panel-header">
              <h2 className="text-sm font-semibold">Recent events</h2>
            </div>
            <div className="new-panel-body space-y-2">
              {(events.data ?? []).slice(0, 8).map((event) => (
                <div className="new-control-event" key={event.id}>
                  <span>{event.message}</span>
                  <em>{event.level}</em>
                </div>
              ))}
              {(events.data ?? []).length === 0 && <div className="text-xs text-slate-500">최근 이벤트가 없습니다.</div>}
            </div>
          </section>
        </aside>
      </section>

      {previewCamera && (
        <CameraPreviewModal camera={previewCamera} onClose={() => setPreviewCamera(null)} />
      )}
    </div>
  );
}

function CameraPreviewModal({ camera, onClose }: { camera: CameraModel; onClose: () => void }) {
  const { videoRef } = useMseStream(liveStreamName(camera));

  return (
    <div className="new-preview-backdrop" role="dialog" aria-modal="true" aria-label={`${camera.name} 미리보기`}>
      <div className="new-preview-modal">
        <div className="new-preview-head">
          <div>
            <div className="text-sm font-semibold text-slate-100">{camera.name}</div>
            <div className="text-xs text-slate-500">{camera.state}</div>
          </div>
          <button className="new-icon-button" type="button" onClick={onClose} aria-label="미리보기 닫기">
            <X size={16} />
          </button>
        </div>
        <video ref={videoRef} className="new-preview-video" muted playsInline autoPlay />
      </div>
    </div>
  );
}

function liveStreamName(camera: CameraModel) {
  return camera.liveStreamName || camera.streamName;
}

function recordingStreamName(camera: CameraModel) {
  return camera.recordingStreamName || camera.streamName;
}

function formatBytes(value: number | undefined) {
  if (!value || value <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(size >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function ControlRoomPage() {
  return <ControlRoomDashboard />;
}
