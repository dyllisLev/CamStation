import {
  CheckCircle2,
  Eye,
  EyeOff,
  Loader2,
  ScanSearch,
} from "lucide-react";
import { type FormEvent, useState } from "react";
import type { DeviceProfile } from "../../app/api";
import { useCreateCamera, usePreviewCamera, useScanCamera } from "../../app/queries";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import {
  candidateLabel,
  defaultRoleSelection,
  initialForm,
  roleLabel,
  selectedCandidate,
  selectionReady,
  streamSelections,
  type CameraFormState,
  type PreviewTarget,
  type RoleSelection,
  toScanRequest,
} from "./model";
import { EmptyState, MutationError } from "./Feedback";
import { ProfileSelectionPanel } from "./ProfileSelectionPanel";

export function CameraProfileRegistration({ onProfileScanned }: { onProfileScanned: (profile: DeviceProfile | null) => void }) {
  const scanCamera = useScanCamera();
  const createCamera = useCreateCamera();
  const previewCamera = usePreviewCamera();
  const [form, setForm] = useState<CameraFormState>(initialForm);
  const [profile, setProfile] = useState<DeviceProfile | null>(null);
  const [selection, setSelection] = useState<RoleSelection>(defaultSelection);
  const [preview, setPreview] = useState<PreviewTarget | null>(null);
  const [showPassword, setShowPassword] = useState(false);

  function updateField<K extends keyof CameraFormState>(field: K, value: CameraFormState[K]) {
    setForm((current) => ({ ...current, [field]: value }));
    if (field !== "password") {
      setProfile(null);
      onProfileScanned(null);
      setPreview(null);
      setSelection(defaultSelection);
    }
  }

  async function onScan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = await scanCamera.mutateAsync(toScanRequest(form));
    setProfile(result.profile);
    onProfileScanned(result.profile);
    setSelection(defaultRoleSelection(result.profile));
    setPreview(null);
  }

  async function onRegister() {
    if (!profile || !selectionReady(selection)) return;
    await createCamera.mutateAsync({
      ...toScanRequest(form),
      name: form.name.trim() || "카메라",
      streamName: form.streamName.trim() || undefined,
      channelIndex: selection.channelIndex,
      streamSelections: streamSelections(selection),
    });
  }

  async function onPreview(role: "recording" | "live", profileToken: string) {
    if (!profile || !profileToken) return;
    const candidate = selectedCandidate(profile, selection.channelIndex, profileToken);
    const result = await previewCamera.mutateAsync({
      ...toScanRequest(form),
      channelIndex: selection.channelIndex,
      profileToken,
      role,
    });
    setPreview({ streamName: result.streamName, label: candidate ? candidateLabel(candidate) : roleLabel(role) });
  }

  return (
    <section className="new-camera-grid">
      <Panel>
        <PanelHeader>
          <div>
            <h2 className="text-sm font-semibold text-slate-100">프로파일 스캔</h2>
            <p className="mt-1 text-xs text-slate-500">제조사, 모델, PTZ, 녹화/라이브 후보를 먼저 확인합니다.</p>
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
                <input className="new-form-control" value={form.username} onChange={(event) => updateField("username", event.target.value)} autoComplete="username" />
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
              <Button type="button" variant="primary" disabled={!profile || !selectionReady(selection) || createCamera.isPending} onClick={onRegister}>
                {createCamera.isPending ? <Loader2 className="animate-spin" size={16} /> : <CheckCircle2 size={16} />}
                선택 프로필 등록
              </Button>
            </div>
          </form>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader>
          <div>
            <h2 className="text-sm font-semibold text-slate-100">장비 프로파일</h2>
            <p className="mt-1 text-xs text-slate-500">미리보기 후 녹화/라이브 프로필을 선택합니다.</p>
          </div>
        </PanelHeader>
        <PanelBody>
          {profile ? (
            <ProfileSelectionPanel
              profile={profile}
              selection={selection}
              preview={preview}
              previewPending={previewCamera.isPending}
              previewError={previewCamera.error?.message}
              onSelectionChange={setSelection}
              onPreview={onPreview}
              onClosePreview={() => setPreview(null)}
            />
          ) : (
            <EmptyState />
          )}
        </PanelBody>
      </Panel>
    </section>
  );
}

const defaultSelection: RoleSelection = {
  channelIndex: 0,
  recordingProfileToken: "",
  liveProfileToken: "",
};
