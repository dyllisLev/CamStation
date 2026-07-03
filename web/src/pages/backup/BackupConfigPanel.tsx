import { CheckCircle2, Loader2, Save } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import type { BackupSettings } from "../../app/api";
import { useBackupConfig, useUpdateBackupConfig } from "../../app/backupQueries";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { errorMessage } from "./backupFormat";

const emptyConfig: BackupSettings = {
  enabled: false,
  target: "",
  retentionDays: 30,
  scheduleEnabled: false,
  scheduleIntervalMinutes: 1440,
  protectUnbacked: true,
};

export function BackupConfigPanel() {
  const config = useBackupConfig();
  const updateConfig = useUpdateBackupConfig();
  const [draft, setDraft] = useState<BackupSettings>(emptyConfig);
  const [confirmSave, setConfirmSave] = useState(false);

  useEffect(() => {
    if (config.data) {
      setDraft(config.data);
      setConfirmSave(false);
    }
  }, [config.data]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!confirmSave) {
      setConfirmSave(true);
      return;
    }
    updateConfig.mutate(draft, { onSuccess: () => setConfirmSave(false) });
  }

  return (
    <Panel>
      <PanelHeader>
        <h2 className="text-sm font-semibold">백업 설정</h2>
      </PanelHeader>
      <PanelBody>
        <form className="space-y-3" onSubmit={submit}>
          <label className="flex items-center justify-between gap-3 rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-2">
            <span>
              <span className="block text-xs font-medium text-slate-300">자동 백업</span>
              <span className="mt-1 block text-xs text-slate-500">원격 복사 작업은 확인 후 실행됩니다.</span>
            </span>
            <input
              aria-label="자동 백업"
              checked={draft.enabled}
              className="h-4 w-4 accent-cyan-400"
              type="checkbox"
              onChange={(event) => setDraft((current) => ({ ...current, enabled: event.target.checked }))}
            />
          </label>
          <label className="flex items-center justify-between gap-3 rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-2">
            <span>
              <span className="block text-xs font-medium text-slate-300">스케줄 실행</span>
              <span className="mt-1 block text-xs text-slate-500">주기마다 관리 녹화 저장소를 백업합니다.</span>
            </span>
            <input
              aria-label="스케줄 실행"
              checked={draft.scheduleEnabled}
              className="h-4 w-4 accent-cyan-400"
              type="checkbox"
              onChange={(event) => setDraft((current) => ({ ...current, scheduleEnabled: event.target.checked }))}
            />
          </label>
          <label className="flex items-center justify-between gap-3 rounded-[7px] border border-slate-800 bg-slate-950/40 px-3 py-2">
            <span>
              <span className="block text-xs font-medium text-slate-300">미백업 영상 보호</span>
              <span className="mt-1 block text-xs text-slate-500">용량 정리 시 백업 전 원본 삭제를 막습니다.</span>
            </span>
            <input
              aria-label="미백업 영상 보호"
              checked={draft.protectUnbacked}
              className="h-4 w-4 accent-cyan-400"
              type="checkbox"
              onChange={(event) => setDraft((current) => ({ ...current, protectUnbacked: event.target.checked }))}
            />
          </label>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">rclone 대상</span>
            <input
              className="new-form-control font-mono"
              placeholder="remote:prefix"
              value={draft.target}
              onChange={(event) => {
                setConfirmSave(false);
                setDraft((current) => ({ ...current, target: event.target.value }));
              }}
            />
          </label>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">스케줄 주기(분)</span>
            <input
              className="new-form-control"
              inputMode="numeric"
              min="1"
              type="number"
              value={draft.scheduleIntervalMinutes}
              onChange={(event) => {
                setConfirmSave(false);
                setDraft((current) => ({ ...current, scheduleIntervalMinutes: Number(event.target.value) }));
              }}
            />
          </label>
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">보관일</span>
            <input
              className="new-form-control"
              inputMode="numeric"
              min="1"
              type="number"
              value={draft.retentionDays}
              onChange={(event) => {
                setConfirmSave(false);
                setDraft((current) => ({ ...current, retentionDays: Number(event.target.value) }));
              }}
            />
          </label>
          <Button className="w-full" disabled={config.isLoading || updateConfig.isPending} type="submit" variant="primary">
            {updateConfig.isPending ? <Loader2 className="animate-spin" size={16} /> : <Save size={16} />}
            {confirmSave ? "설정 저장 확인" : "설정 저장"}
          </Button>
          {config.isLoading && <div className="text-xs text-slate-500">설정을 불러오는 중입니다.</div>}
          {config.error && <div className="text-xs text-red-300">{errorMessage(config.error)}</div>}
          {updateConfig.error && <div className="text-xs text-red-300">{errorMessage(updateConfig.error)}</div>}
          {updateConfig.isSuccess && (
            <div className="flex items-center gap-2 text-xs text-emerald-300">
              <CheckCircle2 size={14} />
              백업 설정이 저장되었습니다.
            </div>
          )}
        </form>
      </PanelBody>
    </Panel>
  );
}
