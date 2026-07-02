import {
  CheckCircle2,
  Eye,
  EyeOff,
  Loader2,
  Mic2,
  RadioTower,
  ScanSearch,
  ShieldCheck,
  Siren,
  Speaker,
  Video,
} from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import type { Camera, CameraScanRequest, DeviceProfile, StreamCandidate } from "../app/api";
import { useCameras, useCreateCamera, useScanCamera } from "../app/queries";
import { StatusDot } from "../components/StatusDot";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";
import { cn, formatDate, formatDurationNanos } from "../lib/utils";

type CameraFormState = {
  name: string;
  streamName: string;
  host: string;
  username: string;
  password: string;
  rtspPort: string;
  httpPort: string;
  onvifPort: string;
  adapter: string;
};

const initialForm: CameraFormState = {
  name: "염소장",
  streamName: "goat-yard",
  host: "192.168.0.55",
  username: "admin",
  password: "",
  rtspPort: "10554",
  httpPort: "10080",
  onvifPort: "10080",
  adapter: "auto",
};

export function CamerasPage() {
  const cameras = useCameras();
  const scanCamera = useScanCamera();
  const createCamera = useCreateCamera();
  const rows = useMemo(() => cameras.data ?? [], [cameras.data]);
  const [form, setForm] = useState<CameraFormState>(initialForm);
  const [profile, setProfile] = useState<DeviceProfile | null>(null);
  const [showPassword, setShowPassword] = useState(false);

  const online = rows.filter((camera) => camera.state === "streaming").length;
  const vstarcam = rows.filter((camera) => camera.profileAdapter === "vstarcam").length;
  const roleStreams = rows.reduce((count, camera) => count + (camera.streams?.length ?? 0), 0);

  function updateField<K extends keyof CameraFormState>(field: K, value: CameraFormState[K]) {
    setForm((current) => ({ ...current, [field]: value }));
    if (field !== "password") setProfile(null);
  }

  async function onScan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = await scanCamera.mutateAsync(toScanRequest(form));
    setProfile(result.profile);
  }

  async function onRegister() {
    const request = toScanRequest(form);
    await createCamera.mutateAsync({
      ...request,
      name: form.name.trim() || "카메라",
      streamName: form.streamName.trim() || undefined,
    });
    const result = await scanCamera.mutateAsync(request);
    setProfile(result.profile);
  }

  return (
    <div className="new-camera-admin">
      <section className="new-control-summary" aria-label="카메라 등록 요약">
        <SummaryStat label="등록 카메라" value={`${online}/${rows.length}`} detail="온라인 / 전체" icon={Video} />
        <SummaryStat label="VStarcam" value={String(vstarcam)} detail="감지된 프로파일" icon={ShieldCheck} />
        <SummaryStat label="역할별 스트림" value={String(roleStreams)} detail="녹화 / 라이브 후보" icon={RadioTower} />
        <SummaryStat label="최근 스캔" value={profile?.adapter ?? "-"} detail={profile ? `${profile.manufacturer} ${profile.model}` : "대기 중"} icon={ScanSearch} />
      </section>

      <section className="new-camera-grid">
        <Panel>
          <PanelHeader>
            <div>
              <h2 className="text-sm font-semibold text-slate-100">프로파일 스캔</h2>
              <p className="mt-1 text-xs text-slate-500">ONVIF에서 제조사, 모델, PTZ, 역할별 스트림을 먼저 확인합니다.</p>
            </div>
          </PanelHeader>
          <PanelBody>
            <form className="new-camera-form" onSubmit={onScan}>
              <div className="new-form-row">
                <label>
                  <span>카메라 이름</span>
                  <input className="new-form-control" value={form.name} onChange={(event) => updateField("name", event.target.value)} />
                </label>
                <label>
                  <span>내부 키</span>
                  <input className="new-form-control" value={form.streamName} onChange={(event) => updateField("streamName", event.target.value)} />
                </label>
              </div>
              <label>
                <span>호스트</span>
                <input className="new-form-control" value={form.host} onChange={(event) => updateField("host", event.target.value)} required />
              </label>
              <div className="new-form-row">
                <label>
                  <span>계정</span>
                  <input className="new-form-control" value={form.username} onChange={(event) => updateField("username", event.target.value)} />
                </label>
                <div className="new-field">
                  <span>비밀번호</span>
                  <div className="new-secret-field">
                    <input
                      aria-label="비밀번호"
                      className="new-form-control"
                      value={form.password}
                      onChange={(event) => updateField("password", event.target.value)}
                      type={showPassword ? "text" : "password"}
                      autoComplete="current-password"
                    />
                    <button type="button" onClick={() => setShowPassword((value) => !value)} aria-label={showPassword ? "입력값 숨기기" : "입력값 보기"}>
                      {showPassword ? <EyeOff size={14} /> : <Eye size={14} />}
                    </button>
                  </div>
                </div>
              </div>
              <div className="new-form-row new-form-row-ports">
                <label>
                  <span>RTSP</span>
                  <input className="new-form-control" value={form.rtspPort} onChange={(event) => updateField("rtspPort", event.target.value)} inputMode="numeric" />
                </label>
                <label>
                  <span>HTTP</span>
                  <input className="new-form-control" value={form.httpPort} onChange={(event) => updateField("httpPort", event.target.value)} inputMode="numeric" />
                </label>
                <label>
                  <span>ONVIF</span>
                  <input className="new-form-control" value={form.onvifPort} onChange={(event) => updateField("onvifPort", event.target.value)} inputMode="numeric" />
                </label>
              </div>
              <label>
                <span>어댑터</span>
                <select className="new-form-control" value={form.adapter} onChange={(event) => updateField("adapter", event.target.value)}>
                  <option value="auto">자동 감지</option>
                  <option value="vstarcam">VStarcam</option>
                  <option value="onvif">ONVIF 일반</option>
                </select>
              </label>
              <MutationError message={scanCamera.error?.message ?? createCamera.error?.message} />
              <div className="new-camera-actions">
                <Button type="submit" variant="secondary" disabled={scanCamera.isPending}>
                  {scanCamera.isPending ? <Loader2 className="animate-spin" size={16} /> : <ScanSearch size={16} />}
                  프로파일 스캔
                </Button>
                <Button type="button" variant="primary" disabled={createCamera.isPending} onClick={onRegister}>
                  {createCamera.isPending ? <Loader2 className="animate-spin" size={16} /> : <CheckCircle2 size={16} />}
                  카메라 등록
                </Button>
              </div>
            </form>
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader>
            <div>
              <h2 className="text-sm font-semibold text-slate-100">장비 프로파일</h2>
              <p className="mt-1 text-xs text-slate-500">스캔 결과와 등록될 스트림 구성을 확인합니다.</p>
            </div>
          </PanelHeader>
          <PanelBody>
            {profile ? <ProfilePreview profile={profile} /> : <EmptyState />}
          </PanelBody>
        </Panel>
      </section>

      <Panel>
        <PanelHeader>
          <h2 className="text-sm font-semibold text-slate-100">등록된 카메라</h2>
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
                </tr>
              </thead>
              <tbody>
                {rows.map((camera) => <CameraRow camera={camera} key={camera.id} />)}
                {rows.length === 0 && (
                  <tr>
                    <td className="px-3 py-8 text-center text-slate-500" colSpan={5}>
                      등록된 카메라가 없습니다.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </PanelBody>
      </Panel>
    </div>
  );
}

function SummaryStat({ label, value, detail, icon: Icon }: { label: string; value: string; detail: string; icon: typeof Video }) {
  return (
    <div className="new-control-stat">
      <div className="new-feature-icon"><Icon size={17} /></div>
      <div>
        <div className="new-control-stat-label">{label}</div>
        <div className="new-control-stat-value">{value}</div>
        <div className="new-control-stat-detail">{detail}</div>
      </div>
    </div>
  );
}

function ProfilePreview({ profile }: { profile: DeviceProfile }) {
  const candidates = profile.channels.flatMap((channel) => channel.candidates);
  return (
    <div className="new-profile-preview">
      <div className="new-profile-identity">
        <div>
          <span>제조사</span>
          <strong>{profile.manufacturer}</strong>
        </div>
        <div>
          <span>모델</span>
          <strong>{profile.model}</strong>
        </div>
        <div>
          <span>어댑터</span>
          <strong>{profile.adapter}</strong>
        </div>
      </div>
      <div className="new-capability-strip">
        <Capability enabled={profile.capabilities.ptz} label={`PTZ${profile.capabilities.maxPresets ? ` ${profile.capabilities.maxPresets}` : ""}`} icon={ScanSearch} />
        <Capability enabled={profile.capabilities.audio} label="오디오" icon={RadioTower} />
        <Capability enabled={profile.capabilities.microphone} label="마이크" icon={Mic2} />
        <Capability enabled={profile.capabilities.speaker} label="스피커" icon={Speaker} />
        <Capability enabled={profile.capabilities.siren} label="사이렌" icon={Siren} />
      </div>
      <div className="new-stream-list" aria-label="역할별 스트림">
        {candidates.map((candidate) => <StreamCandidateRow candidate={candidate} key={`${candidate.profileToken}-${candidate.roleHint}`} />)}
      </div>
    </div>
  );
}

function Capability({ enabled, label, icon: Icon }: { enabled: boolean; label: string; icon: typeof Video }) {
  return (
    <span className={cn("new-capability", enabled && "new-enabled")}>
      <Icon size={13} />
      {label}
    </span>
  );
}

function StreamCandidateRow({ candidate }: { candidate: StreamCandidate }) {
  return (
    <div className="new-stream-row">
      <div>
        <strong>{roleLabel(candidate.roleHint)} · {candidate.label}</strong>
        <span>{candidate.redactedUrl ?? "-"}</span>
      </div>
      <em>{candidate.codec || "codec"} {formatSize(candidate.width, candidate.height)} {candidate.fps ? `${candidate.fps}fps` : ""}</em>
    </div>
  );
}

function CameraRow({ camera }: { camera: Camera }) {
  const streams = camera.streams ?? [];
  return (
    <tr>
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
    </tr>
  );
}

function EmptyState() {
  return (
    <div className="new-empty">
      호스트와 계정을 입력한 뒤 프로파일 스캔을 실행하세요.
    </div>
  );
}

function MutationError({ message }: { message?: string }) {
  if (!message) return null;
  return <div className="new-form-error">{message}</div>;
}

function toScanRequest(form: CameraFormState): CameraScanRequest {
  return {
    name: form.name.trim() || undefined,
    host: form.host.trim(),
    username: form.username.trim() || undefined,
    password: form.password || undefined,
    rtspPort: parsePort(form.rtspPort),
    httpPort: parsePort(form.httpPort),
    onvifPort: parsePort(form.onvifPort),
    adapter: form.adapter,
  };
}

function parsePort(value: string) {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function roleLabel(role: string) {
  if (role === "recording") return "녹화";
  if (role === "live") return "라이브";
  if (role === "snapshot") return "스냅샷";
  return role;
}

function formatSize(width?: number, height?: number) {
  if (!width || !height) return "-";
  return `${width}x${height}`;
}
