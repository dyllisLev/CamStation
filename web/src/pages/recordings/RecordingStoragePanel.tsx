import { ArchiveX, HardDrive, Loader2, Radio, RotateCcw, ScissorsLineDashed, Settings } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import type { UseMutationResult, UseQueryResult } from "@tanstack/react-query";
import type { CleanupResult, RecordingStorage } from "../../app/api";
import { withAppBase } from "../../app/basePath";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { bytesToGB, formatBytes } from "./recordingUtils";

type RecordingStoragePanelProps = {
  readonly storage: UseQueryResult<RecordingStorage, Error>;
  readonly cleanup: UseMutationResult<CleanupResult, Error, number>;
};

export function RecordingStoragePanel({ storage, cleanup }: RecordingStoragePanelProps) {
  const currentMaxGB = bytesToGB(storage.data?.maxBytes);
  const defaultManualGB = currentMaxGB > 0 ? currentMaxGB.toFixed(2) : "0.30";
  const lastSyncedManualGB = useRef(defaultManualGB);
  const [manualGB, setManualGB] = useState(defaultManualGB);
  const [cleanupArmed, setCleanupArmed] = useState(false);
  const maxBytes = storage.data?.maxBytes ?? 0;
  const recordingsBytes = storage.data?.recordingsBytes ?? 0;
  const tempBytes = storage.data?.tempBytes ?? 0;
  const usageRatio = maxBytes > 0 ? Math.min(100, (recordingsBytes / maxBytes) * 100) : 0;
  const totalBytes = recordingsBytes + tempBytes;
  const manualBytes = useMemo(() => Math.max(0, Number(manualGB) * 1024 * 1024 * 1024), [manualGB]);
  useEffect(() => {
    if (defaultManualGB === lastSyncedManualGB.current) {
      return;
    }
    const previousManualGB = lastSyncedManualGB.current;
    lastSyncedManualGB.current = defaultManualGB;
    if (manualGB === previousManualGB) {
      setManualGB(defaultManualGB);
    }
  }, [defaultManualGB, manualGB]);

  const runCleanup = () => {
    if (!cleanupArmed) {
      setCleanupArmed(true);
      return;
    }
    cleanup.mutate(Math.floor(manualBytes), {
      onSettled: () => setCleanupArmed(false),
    });
  };

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
      <Panel>
        <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">녹화 저장소</h2>
            <div className="mt-1 text-xs text-slate-500">완료 파일과 임시 파일을 분리해 감시합니다.</div>
          </div>
          <Badge value={storage.data?.autoCleanupEnabled ? "running" : "warning"} />
        </PanelHeader>
        <PanelBody className="space-y-4">
          {storage.isLoading && <div className="text-sm text-slate-400">저장소 정보를 불러오는 중입니다.</div>}
          {storage.error && <div className="text-sm text-red-300">저장소 정보를 불러오지 못했습니다: {storage.error.message}</div>}
          <div className="grid gap-3 md:grid-cols-3">
            <MetricCard icon={HardDrive} label="완료 녹화" value={formatBytes(recordingsBytes)} />
            <MetricCard icon={ScissorsLineDashed} label="녹화 중 임시" value={formatBytes(tempBytes)} />
            <MetricCard icon={ArchiveX} label="전체 사용량" value={formatBytes(totalBytes)} />
          </div>
          <div className="new-feature-card space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <div className="text-sm font-semibold text-slate-100">자동삭제 기준</div>
                <div className="mt-1 text-xs text-slate-500">
                  {maxBytes > 0 ? `${formatBytes(maxBytes)} 초과 시 오래된 완료 녹화부터 삭제` : "자동삭제가 꺼져 있습니다."}
                </div>
              </div>
              <div className="font-mono text-xs text-slate-400">{maxBytes > 0 ? `${usageRatio.toFixed(1)}%` : "OFF"}</div>
            </div>
            <div className="h-3 overflow-hidden rounded-[7px] border border-slate-700 bg-slate-950">
              <div className="h-full bg-cyan-400 transition-[width]" style={{ width: `${maxBytes > 0 ? usageRatio : 0}%` }} />
            </div>
          </div>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader className="flex items-center gap-2">
          <Radio size={16} className="text-cyan-300" />
          <h2 className="text-sm font-semibold">보존/설정</h2>
        </PanelHeader>
        <PanelBody className="space-y-3">
          <MetricCard label="세그먼트 길이" value="설정 페이지 기준" />
          <MetricCard label="삭제 대상" value="완료 ready 파일" />
          <label className="block space-y-2">
            <span className="text-xs font-medium text-slate-400">이번 정리 목표 GB</span>
            <input className="new-form-control" inputMode="decimal" min="0.01" step="0.01" value={manualGB} onChange={(event) => setManualGB(event.target.value)} />
          </label>
          <Button className="w-full" variant={cleanupArmed ? "danger" : "primary"} disabled={cleanup.isPending || manualBytes <= 0} onClick={runCleanup}>
            {cleanup.isPending ? <Loader2 className="animate-spin" size={16} /> : <RotateCcw size={16} />}
            {cleanupArmed ? "정리 확인" : "지금 정리 실행"}
          </Button>
          {cleanupArmed && (
            <Button className="w-full" variant="ghost" disabled={cleanup.isPending} onClick={() => setCleanupArmed(false)}>
              정리 취소
            </Button>
          )}
          <Button asChild className="w-full" variant="secondary">
            <a href={withAppBase("/settings")}>
              <Settings size={16} />
              녹화 설정 열기
            </a>
          </Button>
          {cleanup.data && (
            <div className="space-y-1 text-xs">
              <div className={cleanup.data.afterBytes > cleanup.data.maxBytes ? "text-amber-300" : "text-emerald-300"}>
                정리 결과: 삭제 {cleanup.data.deleted.length}개, {formatBytes(cleanup.data.beforeBytes)}에서 {formatBytes(cleanup.data.afterBytes)}
              </div>
              {cleanup.data.afterBytes > cleanup.data.maxBytes && cleanup.data.backupProtectionActive && (
                <div className="text-amber-300">
                  미백업 영상 보호로 {formatBytes(cleanup.data.protectedUnbackedBytes)} ({cleanup.data.protectedUnbackedCount}개)를 보존했습니다.
                </div>
              )}
            </div>
          )}
          {cleanup.error && <div className="text-xs text-red-300">정리에 실패했습니다: {cleanup.error.message}</div>}
        </PanelBody>
      </Panel>
    </div>
  );
}

function MetricCard({ icon: Icon, label, value }: { readonly icon?: typeof HardDrive; readonly label: string; readonly value: string }) {
  return (
    <div className="new-feature-card flex items-center justify-between gap-3">
      <div className="min-w-0">
        <div className="text-xs text-slate-500">{label}</div>
        <div className="mt-1 break-words text-sm font-semibold text-slate-100">{value}</div>
      </div>
      {Icon && (
        <div className="new-feature-icon">
          <Icon size={17} />
        </div>
      )}
    </div>
  );
}
