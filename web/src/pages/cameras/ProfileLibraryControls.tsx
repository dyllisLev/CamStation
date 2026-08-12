import { Edit3, Loader2, Trash2 } from "lucide-react";
import type { CameraProfileTemplate } from "../../app/api";
import { Button } from "../../components/ui/button";
import { profileTemplateSummary } from "./profileLibraryModel";

export function TemplateRow({
  template,
  selected,
  confirming,
  pending,
  onEdit,
  onArmDelete,
  onCancelDelete,
  onDelete,
}: {
  readonly template: CameraProfileTemplate;
  readonly selected: boolean;
  readonly confirming: boolean;
  readonly pending: boolean;
  readonly onEdit: (template: CameraProfileTemplate) => void;
  readonly onArmDelete: () => void;
  readonly onCancelDelete: () => void;
  readonly onDelete: (template: CameraProfileTemplate) => void;
}) {
  return (
    <div className={selected ? "new-profile-template-row new-selected" : "new-profile-template-row"}>
      <div>
        <strong>{template.profileName}</strong>
        <span>{template.manufacturer} / {template.model} / {template.adapter} v{template.version}</span>
        <em>{profileTemplateSummary(template)}</em>
      </div>
      <div className="new-profile-template-actions">
        <Button type="button" size="sm" variant="secondary" onClick={() => onEdit(template)}>
          <Edit3 size={14} />
          수정
        </Button>
        {confirming ? (
          <>
            <Button type="button" size="sm" variant="danger" disabled={pending} onClick={() => onDelete(template)}>
              {pending ? <Loader2 className="animate-spin" size={14} /> : <Trash2 size={14} />}
              삭제 확인
            </Button>
            <Button type="button" size="sm" variant="ghost" onClick={onCancelDelete}>
              취소
            </Button>
          </>
        ) : (
          <Button type="button" size="sm" variant="danger" onClick={onArmDelete}>
            <Trash2 size={14} />
            삭제
          </Button>
        )}
      </div>
    </div>
  );
}

export function ProfileTextField({
  label,
  value,
  onChange,
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
}) {
  return (
    <label>
      <span>{label}</span>
      <input className="new-form-control" value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

export function CapabilityToggle({
  label,
  checked,
  onChange,
}: {
  readonly label: string;
  readonly checked: boolean;
  readonly onChange: (value: boolean) => void;
}) {
  return (
    <label>
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      <span>{label}</span>
    </label>
  );
}
