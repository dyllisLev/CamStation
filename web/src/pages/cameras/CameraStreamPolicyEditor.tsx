import { Loader2, RefreshCw, RotateCcw, Save, ScanSearch } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api, type Camera } from "../../app/api";
import { HttpError } from "../../app/http";
import {
  useProbeAllCameraStreamOutputs,
  useProbeCameraStreamOutputs,
  useReapplyCameraStreamOutputs,
  useUpdateCameraStreamOutputs,
} from "../../app/queries";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { StreamOutputPolicyForm } from "./StreamOutputPolicyForm";
import {
  draftFromCamera,
  policyMutationNotice,
  recommendedStreamOutputs,
  reconcilePolicyDraft,
  reloadedPolicyDraft,
  streamOutputUpdateRequest,
  validateStreamOutputs,
  type PolicyMutationNotice,
} from "./streamOutputPolicyModel";

export function CameraStreamPolicyEditor({ camera }: { camera: Camera }) {
  const queryClient = useQueryClient();
  const save = useUpdateCameraStreamOutputs();
  const probe = useProbeCameraStreamOutputs();
  const reapply = useReapplyCameraStreamOutputs();
  const probeAll = useProbeAllCameraStreamOutputs();
  const [draft, setDraft] = useState(() => draftFromCamera(camera));
  const [notice, setNotice] = useState<PolicyMutationNotice | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const [reloading, setReloading] = useState(false);
  const pending = save.isPending || probe.isPending || reapply.isPending || probeAll.isPending || reloading;
  const serverChanged = draft.serverRevision !== draft.baseRevision;
  const availableSourceKeys = [...new Set((camera.streams ?? []).map((item) => item.sourceKey))];

  useEffect(() => {
    setDraft((current) => reconcilePolicyDraft(current, camera));
  }, [camera]);

  async function reloadServer() {
    setReloading(true);
    try {
      await queryClient.invalidateQueries({ queryKey: ["cameras"] });
      const cameras = await queryClient.fetchQuery({ queryKey: ["cameras"], queryFn: api.cameras, staleTime: 0 });
      const fresh = reloadedPolicyDraft(cameras, camera.streamName);
      if (!fresh) throw new Error("서버에서 카메라 설정을 찾을 수 없습니다.");
      setDraft(fresh);
      setNotice(null);
      setValidation(null);
    } catch (cause) {
      setValidation(cause instanceof Error ? cause.message : "서버 설정을 다시 불러오지 못했습니다.");
    } finally {
      setReloading(false);
    }
  }

  async function saveAndApply() {
    const error = validateStreamOutputs(draft.outputs, availableSourceKeys);
    setValidation(error);
    if (error) return;
    try {
      const response = await save.mutateAsync({ streamName: camera.streamName, input: streamOutputUpdateRequest(draft) });
      setDraft(draftFromCamera(response.camera));
      setNotice(policyMutationNotice(response));
    } catch (cause) {
      if (cause instanceof HttpError && cause.status === 409) {
        setNotice(policyMutationNotice(undefined, 409));
        await queryClient.invalidateQueries({ queryKey: ["cameras"] });
      }
    }
  }

  async function probeCamera() {
    const response = await probe.mutateAsync(camera.streamName);
    setNotice(policyMutationNotice(response));
  }

  async function reapplyCamera() {
    try {
      const response = await reapply.mutateAsync({ streamName: camera.streamName, expectedDesiredRevision: draft.baseRevision });
      setNotice(policyMutationNotice(response));
    } catch (cause) {
      if (cause instanceof HttpError && cause.status === 409) {
        setNotice(policyMutationNotice(undefined, 409));
        await queryClient.invalidateQueries({ queryKey: ["cameras"] });
      }
    }
  }

  async function probeEveryCamera() {
    const response = await probeAll.mutateAsync(undefined);
    setNotice(response.applied
      ? { state: "applied", message: "전체 카메라 실제 입력 검사가 완료되었습니다." }
      : { state: "pending", message: response.warning || "검사는 저장되었지만 일부 런타임 적용이 대기 중입니다." });
  }

  const mutationError = save.error ?? probe.error ?? reapply.error ?? probeAll.error;
  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-slate-100">카메라별 스트림 정책</h2>
          <p className="mt-1 text-xs text-slate-500">{camera.name} · DB revision {draft.baseRevision} / applied {camera.streamApplyState.appliedRevision}</p>
        </div>
        <span className={`new-policy-apply-state new-${camera.streamApplyState.state}`}>{applyStateLabel(camera.streamApplyState.state)}</span>
      </PanelHeader>
      <PanelBody className="space-y-3">
        {(serverChanged || notice?.state === "conflict") && (
          <div className="new-policy-warning">
            <span>서버 revision {draft.serverRevision}이 현재 초안 기준 {draft.baseRevision}과 다릅니다.</span>
            <Button type="button" size="sm" variant="secondary" disabled={reloading} onClick={() => void reloadServer()}>
              {reloading && <Loader2 className="animate-spin" size={14} />} 서버값 다시 불러오기
            </Button>
          </div>
        )}
        {notice && <div className={`new-policy-notice new-${notice.state}`}>{notice.message}</div>}
        {camera.streamApplyState.error && <div className="new-policy-error">{camera.streamApplyState.error}</div>}
        {validation && <div className="new-form-error">{validation}</div>}
        {mutationError && !(mutationError instanceof HttpError && mutationError.status === 409) && <div className="new-form-error">{mutationError.message}</div>}
        <StreamOutputPolicyForm
          outputs={draft.outputs}
          statusOutputs={camera.streamOutputs}
          availableSourceKeys={availableSourceKeys}
          disabled={pending}
          onChange={(outputs) => {
            setDraft((current) => ({ ...current, outputs, dirty: true }));
            setNotice(null);
            setValidation(null);
          }}
        />
        <div className="new-policy-actions">
          <Button type="button" variant="primary" disabled={!draft.dirty || pending} onClick={() => void saveAndApply()}>
            {save.isPending ? <Loader2 className="animate-spin" size={15} /> : <Save size={15} />} 저장 및 즉시 적용
          </Button>
          <Button type="button" variant="secondary" disabled={pending} onClick={() => void probeCamera()}>
            {probe.isPending ? <Loader2 className="animate-spin" size={15} /> : <ScanSearch size={15} />} 실제 입력 다시 검사
          </Button>
          <Button type="button" variant="secondary" disabled={pending} onClick={() => setDraft((current) => ({ ...current, outputs: recommendedStreamOutputs(camera.streams?.some((item) => item.sourceKey === "live") ?? false), dirty: true }))}>
            <RotateCcw size={15} /> 권장값 복원
          </Button>
          <Button type="button" variant="secondary" disabled={pending} onClick={() => void reapplyCamera()}>
            {reapply.isPending ? <Loader2 className="animate-spin" size={15} /> : <RefreshCw size={15} />} 다시 적용
          </Button>
          <Button type="button" variant="secondary" disabled={pending} onClick={() => void probeEveryCamera()}>
            {probeAll.isPending ? <Loader2 className="animate-spin" size={15} /> : <ScanSearch size={15} />} 전체 다시 검사
          </Button>
        </div>
      </PanelBody>
    </Panel>
  );
}

function applyStateLabel(state: Camera["streamApplyState"]["state"]): string {
  if (state === "applied") return "적용됨";
  if (state === "pending") return "적용 대기";
  return "적용 실패";
}
