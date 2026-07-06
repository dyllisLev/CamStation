import { Library, Loader2, Plus, ScanLine } from "lucide-react";
import { type FormEvent, useMemo, useState } from "react";
import type { CameraProfileTemplate } from "../../app/api";
import {
  useCameraProfileTemplates,
  useCreateCameraProfile,
  useDeleteCameraProfile,
  useUpdateCameraProfile,
} from "../../app/queries";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { MutationError } from "./Feedback";
import { CapabilityToggle, ProfileTextField, TemplateRow } from "./ProfileLibraryControls";
import { ProfileLibraryValidationError } from "./profileLibraryErrors";
import {
  emptyProfileForm,
  formFromDraftSource,
  formFromTemplate,
  profileInputFromForm,
  type ProfileLibraryFormState,
  type ProfileTemplateDraftSource,
} from "./profileLibraryModel";

type ProfileLibraryProps = {
  readonly draftSource: ProfileTemplateDraftSource | null;
};

export function ProfileLibrary({ draftSource }: ProfileLibraryProps) {
  const templatesQuery = useCameraProfileTemplates();
  const createProfile = useCreateCameraProfile();
  const updateProfile = useUpdateCameraProfile();
  const deleteProfile = useDeleteCameraProfile();
  const templates = useMemo(() => templatesQuery.data ?? [], [templatesQuery.data]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null);
  const [form, setForm] = useState<ProfileLibraryFormState>(emptyProfileForm);
  const [formError, setFormError] = useState("");
  const [deleteError, setDeleteError] = useState("");
  const selectedTemplate = templates.find((template) => template.id === selectedId) ?? null;
  const savePending = createProfile.isPending || updateProfile.isPending;

  function updateField<K extends keyof ProfileLibraryFormState>(field: K, value: ProfileLibraryFormState[K]) {
    setForm((current) => ({ ...current, [field]: value }));
    setFormError("");
  }

  function startCreate() {
    setSelectedId(null);
    setConfirmDeleteId(null);
    setForm(emptyProfileForm);
    setFormError("");
    setDeleteError("");
  }

  function startEdit(template: CameraProfileTemplate) {
    setSelectedId(template.id);
    setConfirmDeleteId(null);
    setForm(formFromTemplate(template));
    setFormError("");
    setDeleteError("");
  }

  function startFromScan() {
    if (!draftSource) return;
    setSelectedId(null);
    setConfirmDeleteId(null);
    setForm(formFromDraftSource(draftSource));
    setFormError("");
    setDeleteError("");
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError("");
    try {
      const input = profileInputFromForm(form);
      if (selectedTemplate) {
        const updated = await updateProfile.mutateAsync({ id: selectedTemplate.id, profile: input });
        setForm(formFromTemplate(updated));
        return;
      }
      const created = await createProfile.mutateAsync(input);
      setSelectedId(created.id);
      setForm(formFromTemplate(created));
    } catch (error) {
      if (error instanceof ProfileLibraryValidationError) {
        setFormError(error.message);
        return;
      }
      throw error;
    }
  }

  async function onDelete(template: CameraProfileTemplate) {
    setDeleteError("");
    try {
      await deleteProfile.mutateAsync(template.id);
      if (selectedId === template.id) {
        startCreate();
      }
      setConfirmDeleteId(null);
    } catch (error) {
      if (error instanceof Error) {
        setDeleteError(localizeDeleteError(error.message));
        return;
      }
      throw error;
    }
  }

  return (
    <section className="new-profile-library-grid" aria-label="프로파일 라이브러리">
      <Panel>
        <PanelHeader className="new-profile-library-header">
          <div>
            <h2 className="text-sm font-semibold text-slate-100">프로파일 라이브러리</h2>
            <p className="mt-1 text-xs text-slate-500">재사용 제조사/모델 템플릿</p>
          </div>
          <Library size={17} aria-hidden="true" />
        </PanelHeader>
        <PanelBody>
          <div className="new-profile-library-actions">
            <Button type="button" variant="secondary" onClick={startCreate}>
              <Plus size={15} />
              새 프로파일
            </Button>
            <Button type="button" variant="secondary" disabled={!draftSource} onClick={startFromScan}>
              <ScanLine size={15} />
              스캔 선택으로 작성
            </Button>
          </div>
          <MutationError message={templatesQuery.error?.message ?? deleteError} />
          {templates.length === 0 ? (
            <div className="new-empty-inline">저장된 프로파일 템플릿이 없습니다.</div>
          ) : (
            <div className="new-profile-template-list">
              {templates.map((template) => (
                <TemplateRow
                  key={template.id}
                  template={template}
                  selected={selectedId === template.id}
                  confirming={confirmDeleteId === template.id}
                  pending={deleteProfile.isPending}
                  onEdit={startEdit}
                  onArmDelete={() => setConfirmDeleteId(template.id)}
                  onCancelDelete={() => setConfirmDeleteId(null)}
                  onDelete={onDelete}
                />
              ))}
            </div>
          )}
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader>
          <div>
            <h2 className="text-sm font-semibold text-slate-100">
              {selectedTemplate ? "프로파일 수정" : "프로파일 생성"}
            </h2>
            <p className="mt-1 text-xs text-slate-500">자격 증명 없는 채널 매핑</p>
          </div>
        </PanelHeader>
        <PanelBody>
          <form className="new-camera-form" onSubmit={onSubmit}>
            <div className="new-form-row">
              <ProfileTextField label="프로파일명" value={form.profileName} onChange={(value) => updateField("profileName", value)} />
              <ProfileTextField label="어댑터" value={form.adapter} onChange={(value) => updateField("adapter", value)} />
            </div>
            <div className="new-form-row">
              <ProfileTextField label="제조사" value={form.manufacturer} onChange={(value) => updateField("manufacturer", value)} />
              <ProfileTextField label="모델" value={form.model} onChange={(value) => updateField("model", value)} />
            </div>
            <div className="new-form-row">
              <ProfileTextField label="버전" value={form.version} onChange={(value) => updateField("version", value)} />
              <div className="new-profile-library-toggles" aria-label="프로파일 기능">
                <CapabilityToggle label="ONVIF" checked={form.onvif} onChange={(value) => updateField("onvif", value)} />
                <CapabilityToggle label="RTSP" checked={form.rtsp} onChange={(value) => updateField("rtsp", value)} />
                <CapabilityToggle label="스냅샷" checked={form.snapshot} onChange={(value) => updateField("snapshot", value)} />
                <CapabilityToggle label="다중 채널" checked={form.multiChannel} onChange={(value) => updateField("multiChannel", value)} />
              </div>
            </div>
            <label>
              <span>채널 매핑</span>
              <textarea
                className="new-form-control new-profile-mapping-input"
                value={form.mappingText}
                onChange={(event) => updateField("mappingText", event.target.value)}
                spellCheck={false}
              />
            </label>
            <MutationError message={formError || createProfile.error?.message || updateProfile.error?.message} />
            <div className="new-camera-actions">
              <Button type="submit" variant="primary" disabled={savePending}>
                {savePending ? <Loader2 className="animate-spin" size={15} /> : <Plus size={15} />}
                {selectedTemplate ? "프로파일 저장" : "프로파일 생성"}
              </Button>
              <Button type="button" variant="secondary" onClick={startCreate}>
                입력 초기화
              </Button>
            </div>
          </form>
        </PanelBody>
      </Panel>
    </section>
  );
}

function localizeDeleteError(message: string): string {
  if (message.includes("referenced") || message.includes("in use")) {
    return "이 프로파일은 등록된 카메라에서 사용 중이어서 삭제할 수 없습니다.";
  }
  return `프로파일 삭제 실패: ${message}`;
}
