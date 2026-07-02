import type { Camera, CameraStream } from "../../app/api";
import { Badge } from "../../components/ui/badge";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { formatSize, roleLabel } from "./model";

export function RegisteredCameraProfile({ camera }: { camera: Camera | null }) {
  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-slate-100">프로파일 설정</h2>
          <p className="mt-1 text-xs text-slate-500">
            {camera ? camera.name : "카메라를 선택하면 역할별 스트림 세부 정보가 표시됩니다."}
          </p>
        </div>
        {camera && <Badge value={camera.state} />}
      </PanelHeader>
      <PanelBody>
        {camera ? (
          <div className="new-registered-profile">
            <div className="new-profile-facts">
              <ProfileFact label="제조사" value={camera.manufacturer || "-"} />
              <ProfileFact label="모델" value={camera.model || "-"} />
              <ProfileFact label="어댑터" value={camera.profileAdapter || "legacy"} />
              <ProfileFact label="채널" value={camera.channelIndex === undefined ? "-" : String(camera.channelIndex)} />
              <ProfileFact label="녹화 스트림" value={camera.recordingStreamName || streamNameForRole(camera, "recording") || "-"} />
              <ProfileFact label="라이브 스트림" value={camera.liveStreamName || streamNameForRole(camera, "live") || "-"} />
            </div>
            <div className="new-registered-streams">
              {(camera.streams ?? []).map((stream) => (
                <RegisteredStream stream={stream} key={`${stream.role}-${stream.go2rtcStreamName}`} />
              ))}
              {!camera.streams?.length && (
                <div className="new-empty-inline">역할 스트림 정보가 아직 없습니다.</div>
              )}
            </div>
          </div>
        ) : (
          <div className="new-empty-inline">등록된 카메라가 없습니다.</div>
        )}
      </PanelBody>
    </Panel>
  );
}

function ProfileFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="new-profile-fact">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function RegisteredStream({ stream }: { stream: CameraStream }) {
  return (
    <div className="new-registered-stream">
      <div>
        <div className="new-registered-stream-head">
          <strong>{roleLabel(stream.role)}</strong>
          <Badge value={stream.state ?? "unknown"} />
        </div>
        <span>{stream.go2rtcStreamName}</span>
        <em>{streamDetail(stream)}</em>
      </div>
      <code>{stream.profileToken || "-"}</code>
    </div>
  );
}

function streamNameForRole(camera: Camera, role: string): string | undefined {
  return camera.streams?.find((stream) => stream.role === role)?.go2rtcStreamName;
}

function streamDetail(stream: CameraStream): string {
  return [
    stream.label,
    formatSize(stream.width, stream.height),
    stream.codec,
    stream.fps ? `${stream.fps}fps` : "",
  ]
    .filter(Boolean)
    .join(" · ");
}
