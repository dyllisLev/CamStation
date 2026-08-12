import { Loader2, Trash2, XCircle } from "lucide-react";
import { Button } from "../../components/ui/button";

type RegisteredCameraDeleteControlsProps = {
  confirming: boolean;
  pending: boolean;
  onArm: () => void;
  onCancel: () => void;
  onDelete: () => void;
};

export function RegisteredCameraDeleteControls({
  confirming,
  pending,
  onArm,
  onCancel,
  onDelete,
}: RegisteredCameraDeleteControlsProps) {
  if (!confirming) {
    return (
      <Button type="button" variant="danger" onClick={onArm}>
        <Trash2 size={16} />
        카메라 삭제
      </Button>
    );
  }
  return (
    <div className="new-delete-confirm">
      <span>삭제하면 등록 목록과 역할 스트림에서 제거됩니다.</span>
      <div>
        <Button type="button" variant="danger" disabled={pending} onClick={onDelete}>
          {pending ? <Loader2 className="animate-spin" size={16} /> : <Trash2 size={16} />}
          삭제 확정
        </Button>
        <Button type="button" variant="secondary" disabled={pending} onClick={onCancel}>
          <XCircle size={16} />
          취소
        </Button>
      </div>
    </div>
  );
}
