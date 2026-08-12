import { Loader2, Plus, ScanSearch, Save } from "lucide-react";
import { type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import type { Camera, CameraProfileMatch, DeviceProfile, StreamOutputSettingsTuple } from "../../app/api";
import {
  useCameraProfileTemplates,
  useCreateCamera,
  useCreateCameraProfile,
  useDeleteCamera,
  usePreviewCamera,
  usePreviewRegisteredCamera,
  useScanCamera,
  useScanRegisteredCamera,
  useUpdateCamera,
} from "../../app/queries";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { EmptyState, MutationError } from "./Feedback";
import { ProfileSelectionPanel } from "./ProfileSelectionPanel";
import { RegisteredCameraDeleteControls } from "./RegisteredCameraDeleteControls";
import { RegisteredCameraStoredProfile } from "./RegisteredCameraStoredProfile";
import { ConnectionFields } from "./CameraWorkflowFields";
import { MatchList } from "./CameraWorkflowMatches";
import {
  candidateLabel,
  candidatesForChannel,
  defaultRoleSelection,
  initialForm,
  roleLabel,
  selectedCandidate,
  selectionReady,
  toScanRequest,
  type CameraFormState,
  type PreviewTarget,
  type RoleSelection,
} from "./model";
import { cameraPayload, formFromCamera, type WorkflowMode } from "./CameraWorkflowModel";
import { StreamOutputPolicyForm } from "./StreamOutputPolicyForm";
import { draftFromCamera, hasDistinctLiveSource, normalizeUnavailableSources, recommendedStreamOutputs, validateStreamOutputs } from "./streamOutputPolicyModel";
import type { ProfileTemplateDraftSource } from "./profileLibraryModel";
import { formFromDraftSource, profileInputFromForm } from "./profileLibraryModel";

type CameraWorkflowProps = {
  mode: WorkflowMode;
  camera: Camera | null;
  onScanComplete?: (profile: DeviceProfile | null) => void;
  onProfileDraftChange?: (source: ProfileTemplateDraftSource | null) => void;
  onDeleted?: () => void;
};

const defaultSelection: RoleSelection = {
  channelIndex: 0,
  recordingProfileToken: "",
  liveProfileToken: "",
};

export function CameraWorkflow({ mode, camera, onScanComplete, onProfileDraftChange, onDeleted }: CameraWorkflowProps) {
  const profileTemplates = useCameraProfileTemplates();
  const scanCamera = useScanCamera();
  const scanRegisteredCamera = useScanRegisteredCamera();
  const previewCamera = usePreviewCamera();
  const previewRegisteredCamera = usePreviewRegisteredCamera();
  const createCamera = useCreateCamera();
  const createProfile = useCreateCameraProfile();
  const updateCamera = useUpdateCamera();
  const deleteCamera = useDeleteCamera();
  const [form, setForm] = useState<CameraFormState>(mode === "edit" && camera ? formFromCamera(camera) : initialForm);
  const [scan, setScan] = useState<DeviceProfile | null>(null);
  const [matches, setMatches] = useState<readonly CameraProfileMatch[]>([]);
  const [selectedTemplateId, setSelectedTemplateId] = useState<number | undefined>(camera?.profileTemplateId);
  const [selection, setSelection] = useState<RoleSelection>(defaultSelection);
  const [preview, setPreview] = useState<PreviewTarget | null>(null);
  const [showPassword, setShowPassword] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [streamOutputs, setStreamOutputs] = useState<StreamOutputSettingsTuple>(() => recommendedStreamOutputs(false));
  const [policyValidation, setPolicyValidation] = useState<string | null>(null);
  const [saveNotice, setSaveNotice] = useState<{ warning: boolean; message: string } | null>(null);
  const editMode = mode === "edit" && camera !== null;
  const activeScanPending = scanCamera.isPending || scanRegisteredCamera.isPending;
  const activePreviewPending = previewCamera.isPending || previewRegisteredCamera.isPending;
  const savePending = createCamera.isPending || updateCamera.isPending || createProfile.isPending;
  const templates = useMemo(() => profileTemplates.data ?? [], [profileTemplates.data]);
  const resetCameraRef = useRef(camera);
  if (resetCameraRef.current?.streamName !== camera?.streamName) resetCameraRef.current = camera;
  const resetCamera = resetCameraRef.current;

  useEffect(() => {
    setForm(mode === "edit" && resetCamera ? formFromCamera(resetCamera) : initialForm);
    setScan(null);
    setMatches([]);
    setSelectedTemplateId(mode === "edit" ? resetCamera?.profileTemplateId : undefined);
    setSelection(defaultSelection);
    setPreview(null);
    setShowPassword(false);
    setConfirmDelete(false);
    setStreamOutputs(mode === "edit" && resetCamera ? draftFromCamera(resetCamera).outputs : recommendedStreamOutputs(false));
    setPolicyValidation(null);
    setSaveNotice(null);
    onScanComplete?.(null);
    onProfileDraftChange?.(null);
  }, [mode, resetCamera, onScanComplete, onProfileDraftChange]);

  useEffect(() => {
    onProfileDraftChange?.(scan && selectionReady(selection) ? { profile: scan, selection } : null);
  }, [scan, selection, onProfileDraftChange]);

  function updateField<K extends keyof CameraFormState>(field: K, value: CameraFormState[K]) {
    setForm((current) => ({ ...current, [field]: value }));
    setSaveNotice(null);
    if (field !== "password") {
      setScan(null);
      setMatches([]);
      setSelectedTemplateId(undefined);
      setSelection(defaultSelection);
      setPreview(null);
      onScanComplete?.(null);
      onProfileDraftChange?.(null);
    }
  }

  async function onScan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const request = toScanRequest(form);
    const result = editMode
      ? await scanRegisteredCamera.mutateAsync({ streamName: camera.streamName, camera: request })
      : await scanCamera.mutateAsync(request);
    setScan(result.scan);
    setMatches(result.matches);
    setSelectedTemplateId(result.recommendation?.templateId);
    const nextSelection = defaultRoleSelection(result.scan);
    setSelection(nextSelection);
    const hasLiveSource = selectionHasLiveSource(result.scan, nextSelection);
    if (editMode) {
      setStreamOutputs((current) => normalizeUnavailableSources(current, hasLiveSource ? ["recording", "live"] : ["recording"]));
    } else {
      setStreamOutputs(recommendedStreamOutputs(hasLiveSource));
    }
    setPreview(null);
    onScanComplete?.(result.scan);
  }

  async function onPreview(role: "recording" | "live", profileToken: string) {
    if (!scan || !profileToken) return;
    const candidate = selectedCandidate(scan, selection.channelIndex, profileToken);
    const request = { ...toScanRequest(form), channelIndex: selection.channelIndex, profileToken, role };
    const result = editMode
      ? await previewRegisteredCamera.mutateAsync({ streamName: camera.streamName, camera: request })
      : await previewCamera.mutateAsync(request);
    setPreview({ streamName: result.streamName, label: candidate ? candidateLabel(candidate) : roleLabel(role) });
  }

  async function onSave() {
    if (!scan || !selectionReady(selection)) return;
    const availableSourceKeys = selectionHasLiveSource(scan, selection) ? ["recording", "live"] as const : ["recording"] as const;
    const policyError = validateStreamOutputs(streamOutputs, availableSourceKeys);
    setPolicyValidation(policyError);
    if (policyError) return;
    if (editMode) {
      const result = await updateCamera.mutateAsync({ streamName: camera.streamName, camera: cameraPayload(mode, form, scan, selection, selectedTemplateId, camera, streamOutputs) });
      setSaveNotice(mutationSaveNotice(result.camera, result.warning));
      return;
    }
    const result = await createCamera.mutateAsync(cameraPayload(mode, form, scan, selection, selectedTemplateId, camera, streamOutputs));
    setSaveNotice(mutationSaveNotice(result.camera, result.warning));
    setScan(null);
    setMatches([]);
    setSelectedTemplateId(undefined);
    setSelection(defaultSelection);
    setPreview(null);
    onScanComplete?.(null);
  }

  async function onSaveWithNewProfile() {
    if (!scan || !selectionReady(selection)) return;
    const availableSourceKeys = selectionHasLiveSource(scan, selection) ? ["recording", "live"] as const : ["recording"] as const;
    const policyError = validateStreamOutputs(streamOutputs, availableSourceKeys);
    setPolicyValidation(policyError);
    if (policyError) return;
    const created = await createProfile.mutateAsync(profileInputFromForm(formFromDraftSource({ profile: scan, selection })));
    setSelectedTemplateId(created.id);
    if (editMode) {
      const result = await updateCamera.mutateAsync({ streamName: camera.streamName, camera: cameraPayload(mode, form, scan, selection, created.id, camera, streamOutputs) });
      setSaveNotice(mutationSaveNotice(result.camera, result.warning));
      return;
    }
    const result = await createCamera.mutateAsync(cameraPayload(mode, form, scan, selection, created.id, camera, streamOutputs));
    setSaveNotice(mutationSaveNotice(result.camera, result.warning));
    setScan(null);
    setMatches([]);
    setSelectedTemplateId(undefined);
    setSelection(defaultSelection);
    setPreview(null);
    onScanComplete?.(null);
  }

  async function onDelete() {
    if (!editMode) return;
    await deleteCamera.mutateAsync(camera.streamName);
    setConfirmDelete(false);
    onDeleted?.();
  }

  return (
    <section className="new-camera-grid" data-camera-workflow={mode}>
      <Panel>
        <PanelHeader>
          <div>
            <h2 className="text-sm font-semibold text-slate-100">{editMode ? "카메라 수정" : "카메라 등록"}</h2>
            <p className="mt-1 text-xs text-slate-500">{editMode ? camera.name : "연결 정보를 입력하고 장비 스캔을 실행합니다."}</p>
          </div>
        </PanelHeader>
        <PanelBody>
          <form className="new-camera-form" onSubmit={onScan}>
            <ConnectionFields form={form} showPassword={showPassword} streamNameLocked={editMode} onFieldChange={updateField} onTogglePassword={() => setShowPassword((value) => !value)} />
            <MutationError message={scanCamera.error?.message ?? scanRegisteredCamera.error?.message ?? createProfile.error?.message ?? createCamera.error?.message ?? updateCamera.error?.message} />
            <MutationError message={policyValidation ?? undefined} />
            {saveNotice && <div className={saveNotice.warning ? "new-policy-notice new-pending" : "new-policy-notice new-applied"}>{saveNotice.message}</div>}
            <div className="new-camera-actions">
              <Button type="submit" variant="secondary" disabled={activeScanPending}>
                {activeScanPending ? <Loader2 className="animate-spin" size={16} /> : <ScanSearch size={16} />}
                스캔
              </Button>
              <Button type="button" variant="primary" disabled={!scan || !selectionReady(selection) || savePending} onClick={onSave}>
                {savePending ? <Loader2 className="animate-spin" size={16} /> : editMode ? <Save size={16} /> : <Plus size={16} />}
                {editMode ? "카메라 수정" : "카메라 등록"}
              </Button>
              <Button type="button" variant="secondary" disabled={!scan || !selectionReady(selection) || selectedTemplateId !== undefined || savePending} onClick={onSaveWithNewProfile}>
                {savePending ? <Loader2 className="animate-spin" size={16} /> : <Plus size={16} />}
                프로파일 생성 후 저장
              </Button>
            </div>
          </form>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader>
          <div>
            <h2 className="text-sm font-semibold text-slate-100">프로파일 매칭</h2>
            <p className="mt-1 text-xs text-slate-500">장비 스캔 결과와 저장된 템플릿 매칭을 확인합니다.</p>
          </div>
        </PanelHeader>
        <PanelBody>
          {scan ? (
            <>
              <MatchList matches={matches} selectedTemplateId={selectedTemplateId} templates={templates} onSelect={setSelectedTemplateId} />
              <ProfileSelectionPanel
                profile={scan}
                selection={selection}
                preview={preview}
                previewPending={activePreviewPending}
                previewError={previewCamera.error?.message ?? previewRegisteredCamera.error?.message}
                onSelectionChange={(next) => {
                  setSelection(next);
                  if (!selectionHasLiveSource(scan, next)) {
                    setStreamOutputs((current) => normalizeUnavailableSources(current, ["recording"]));
                  }
                }}
                onPreview={onPreview}
                onClosePreview={() => setPreview(null)}
              />
              <div className="new-registration-policy">
                <div className="new-section-title">
                  <span>{editMode ? "재스캔 후 스트림 정책" : "초기 스트림 정책"}</span>
                  <em>{editMode ? "새 입력과 함께 저장" : "DB 저장 후 즉시 적용"}</em>
                </div>
                <StreamOutputPolicyForm
                  outputs={streamOutputs}
                  availableSourceKeys={selectionHasLiveSource(scan, selection) ? ["recording", "live"] : ["recording"]}
                  onChange={(outputs) => { setStreamOutputs(outputs); setPolicyValidation(null); }}
                />
              </div>
            </>
          ) : editMode ? (
            <RegisteredCameraStoredProfile camera={camera} />
          ) : (
            <EmptyState />
          )}
          {editMode && (
            <RegisteredCameraDeleteControls confirming={confirmDelete} pending={deleteCamera.isPending} onArm={() => setConfirmDelete(true)} onCancel={() => setConfirmDelete(false)} onDelete={onDelete} />
          )}
          <MutationError message={deleteCamera.error?.message} />
        </PanelBody>
      </Panel>
    </section>
  );
}

function mutationSaveNotice(camera: Camera, warning?: string): { warning: boolean; message: string } {
  if (warning || camera.streamApplyState.state !== "applied") {
    return { warning: true, message: warning || "카메라는 저장되었지만 스트림 적용이 대기 중입니다." };
  }
  return { warning: false, message: "카메라와 스트림 정책이 저장 및 적용되었습니다." };
}

function selectionHasLiveSource(profile: DeviceProfile, selection: RoleSelection): boolean {
  return hasDistinctLiveSource(
    candidatesForChannel(profile, selection.channelIndex),
    selection.recordingProfileToken,
    selection.liveProfileToken,
  );
}
