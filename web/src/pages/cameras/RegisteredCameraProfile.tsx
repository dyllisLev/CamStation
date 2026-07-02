import { Loader2, Save } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import type { Camera, DeviceProfile } from "../../app/api";
import { useDeleteCamera, usePreviewRegisteredCamera, useScanRegisteredCamera, useUpdateCamera } from "../../app/queries";
import { Badge } from "../../components/ui/badge";
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
  toScanRequest,
  type CameraFormState,
  type PreviewTarget,
  type RoleSelection,
} from "./model";
import { MutationError } from "./Feedback";
import { ProfileSelectionPanel } from "./ProfileSelectionPanel";
import { RegisteredCameraDeleteControls } from "./RegisteredCameraDeleteControls";
import { RegisteredCameraEditForm } from "./RegisteredCameraEditForm";
import { RegisteredCameraStoredProfile } from "./RegisteredCameraStoredProfile";

export function RegisteredCameraProfile({ camera }: { camera: Camera | null }) {
  const cameraId = camera?.id;
  const cameraName = camera?.name ?? initialForm.name;
  const cameraStreamName = camera?.streamName ?? initialForm.streamName;
  const cameraHost = camera?.host ?? "";
  const cameraRTSPPort = camera?.rtspPort;
  const cameraHTTPPort = camera?.httpPort;
  const cameraONVIFPort = camera?.onvifPort;
  const cameraAdapter = camera?.profileAdapter || "auto";
  const scanCamera = useScanRegisteredCamera();
  const updateCamera = useUpdateCamera();
  const deleteCamera = useDeleteCamera();
  const previewCamera = usePreviewRegisteredCamera();
  const [form, setForm] = useState<CameraFormState>({
    name: cameraName,
    streamName: cameraStreamName,
    host: cameraHost,
    username: "admin",
    password: "",
    rtspPort: String(cameraRTSPPort ?? ""),
    httpPort: String(cameraHTTPPort ?? ""),
    onvifPort: String(cameraONVIFPort ?? ""),
    adapter: cameraAdapter,
  });
  const [profile, setProfile] = useState<DeviceProfile | null>(null);
  const [selection, setSelection] = useState<RoleSelection>(defaultSelection);
  const [preview, setPreview] = useState<PreviewTarget | null>(null);
  const [showPassword, setShowPassword] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    setForm({
      name: cameraName,
      streamName: cameraStreamName,
      host: cameraHost,
      username: "admin",
      password: "",
      rtspPort: String(cameraRTSPPort ?? ""),
      httpPort: String(cameraHTTPPort ?? ""),
      onvifPort: String(cameraONVIFPort ?? ""),
      adapter: cameraAdapter,
    });
    setProfile(null);
    setSelection(defaultSelection);
    setPreview(null);
    setShowPassword(false);
    setConfirmDelete(false);
  }, [
    cameraId,
    cameraName,
    cameraStreamName,
    cameraHost,
    cameraRTSPPort,
    cameraHTTPPort,
    cameraONVIFPort,
    cameraAdapter,
  ]);

  function updateField<K extends keyof CameraFormState>(field: K, value: CameraFormState[K]) {
    setForm((current) => ({ ...current, [field]: value }));
    if (field !== "password") {
      setProfile(null);
      setPreview(null);
      setSelection(defaultSelection);
    }
  }

  async function onScan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!camera) return;
    const result = await scanCamera.mutateAsync({ streamName: camera.streamName, camera: toScanRequest(form) });
    setProfile(result.profile);
    setSelection(defaultRoleSelection(result.profile));
    setPreview(null);
  }

  async function onPreview(role: "recording" | "live", profileToken: string) {
    if (!profile || !profileToken) return;
    const candidate = selectedCandidate(profile, selection.channelIndex, profileToken);
    if (!camera) return;
    const result = await previewCamera.mutateAsync({
      streamName: camera.streamName,
      camera: {
        ...toScanRequest(form),
        channelIndex: selection.channelIndex,
        profileToken,
        role,
      },
    });
    setPreview({ streamName: result.streamName, label: candidate ? candidateLabel(candidate) : roleLabel(role) });
  }

  async function onSave() {
    if (!camera || !profile || !selectionReady(selection)) return;
    await updateCamera.mutateAsync({
      streamName: camera.streamName,
      camera: {
        ...toScanRequest(form),
        name: form.name.trim() || camera.name,
        streamName: camera.streamName,
        channelIndex: selection.channelIndex,
        streamSelections: streamSelections(selection),
      },
    });
  }

  async function onDelete() {
    if (!camera) return;
    await deleteCamera.mutateAsync(camera.streamName);
    setConfirmDelete(false);
  }

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-slate-100">프로파일 수정</h2>
          <p className="mt-1 text-xs text-slate-500">
            {camera ? camera.name : "카메라를 선택하면 역할별 스트림 세부 정보가 표시됩니다."}
          </p>
        </div>
        {camera && <Badge value={camera.state} />}
      </PanelHeader>
      <PanelBody>
        {camera ? (
          <div className="new-edit-profile-grid">
            <RegisteredCameraEditForm
              form={form}
              showPassword={showPassword}
              scanPending={scanCamera.isPending}
              scanError={scanCamera.error?.message}
              onSubmit={onScan}
              onFieldChange={updateField}
              onTogglePassword={() => setShowPassword((value) => !value)}
            />

            <div className="new-registered-profile">
              {profile ? (
                <>
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
                  <Button type="button" variant="primary" disabled={!selectionReady(selection) || updateCamera.isPending} onClick={onSave}>
                    {updateCamera.isPending ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
                    수정 저장
                  </Button>
                </>
              ) : (
                <RegisteredCameraStoredProfile camera={camera} />
              )}
              <MutationError message={updateCamera.error?.message ?? deleteCamera.error?.message} />
              <RegisteredCameraDeleteControls
                confirming={confirmDelete}
                pending={deleteCamera.isPending}
                onArm={() => setConfirmDelete(true)}
                onCancel={() => setConfirmDelete(false)}
                onDelete={onDelete}
              />
            </div>
          </div>
        ) : (
          <div className="new-empty-inline">등록된 카메라가 없습니다.</div>
        )}
      </PanelBody>
    </Panel>
  );
}

const defaultSelection: RoleSelection = {
  channelIndex: 0,
  recordingProfileToken: "",
  liveProfileToken: "",
};
