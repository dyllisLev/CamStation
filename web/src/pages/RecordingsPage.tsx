import { ArchiveX, HardDrive, Loader2, Radio, RotateCcw, ScissorsLineDashed } from "lucide-react";
import { useMemo, useState } from "react";
import { useCleanupRecordings, useRecorderStatus, useRecordingStorage } from "../app/queries";
import { StatusDot } from "../components/StatusDot";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";

export function RecordingsPage() {
  const storage = useRecordingStorage();
  const recorders = useRecorderStatus();
  const cleanup = useCleanupRecordings();
  const currentMaxGB = bytesToGB(storage.data?.maxBytes ?? 0);
  const [manualGB, setManualGB] = useState(currentMaxGB > 0 ? currentMaxGB.toFixed(2) : "0.30");
  const maxBytes = storage.data?.maxBytes ?? 0;
  const recordingsBytes = storage.data?.recordingsBytes ?? 0;
  const tempBytes = storage.data?.tempBytes ?? 0;
  const usageRatio = maxBytes > 0 ? Math.min(100, (recordingsBytes / maxBytes) * 100) : 0;
  const totalBytes = recordingsBytes + tempBytes;
  const activeWorkers = recorders.data?.workers.filter((worker) => worker.state === "running").length ?? 0;
  const manualBytes = useMemo(() => Math.max(0, Number(manualGB) * 1024 * 1024 * 1024), [manualGB]);

  return (
    <div className="space-y-4">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <Panel>
          <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 className="text-sm font-semibold">녹화 저장소</h2>
              <div className="mt-1 text-xs text-slate-500">
                완료 파일은 recordings, 녹화 중 파일은 temp에 분리됩니다.
              </div>
            </div>
            <Badge value={storage.data?.autoCleanupEnabled ? "running" : "warning"} />
          </PanelHeader>
          <PanelBody className="space-y-4">
            <div className="grid gap-3 md:grid-cols-3">
              <MetricCard icon={HardDrive} label="완료 녹화" value={formatBytes(recordingsBytes)} />
              <MetricCard icon={ScissorsLineDashed} label="녹화 중 temp" value={formatBytes(tempBytes)} />
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
                <div className="font-mono text-xs text-slate-400">
                  {maxBytes > 0 ? `${usageRatio.toFixed(1)}%` : "OFF"}
                </div>
              </div>
              <div className="h-3 overflow-hidden rounded-full border border-slate-700 bg-slate-950">
                <div
                  className="h-full bg-cyan-400 transition-[width]"
                  style={{ width: `${maxBytes > 0 ? usageRatio : 0}%` }}
                />
              </div>
              <div className="grid gap-2 text-xs text-slate-500 md:grid-cols-2">
                <div className="truncate">recordings: {storage.data?.recordingsDir ?? "-"}</div>
                <div className="truncate">temp: {storage.data?.tempDir ?? "-"}</div>
              </div>
            </div>
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader>
            <h2 className="text-sm font-semibold">용량 정리</h2>
          </PanelHeader>
          <PanelBody className="space-y-3">
            <label className="block space-y-2">
              <span className="text-xs font-medium text-slate-400">테스트 기준 GB</span>
              <input
                className="new-form-control"
                inputMode="decimal"
                min="0.01"
                step="0.01"
                value={manualGB}
                onChange={(event) => setManualGB(event.target.value)}
              />
            </label>
            <Button
              className="w-full"
              variant="primary"
              disabled={cleanup.isPending || manualBytes <= 0}
              onClick={() => cleanup.mutate(Math.floor(manualBytes))}
            >
              {cleanup.isPending ? <Loader2 className="animate-spin" size={16} /> : <RotateCcw size={16} />}
              지금 정리 실행
            </Button>
            {cleanup.data && (
              <div className="new-feature-card text-xs text-slate-400">
                {formatBytes(cleanup.data.beforeBytes)}에서 {formatBytes(cleanup.data.afterBytes)}로 정리,
                삭제 {cleanup.data.deleted.length}개
              </div>
            )}
            {cleanup.error && <div className="text-xs text-red-300">{cleanup.error.message}</div>}
          </PanelBody>
        </Panel>
      </div>

      <Panel>
        <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">녹화 워커</h2>
            <div className="mt-1 text-xs text-slate-500">
              {activeWorkers} / {recorders.data?.workers.length ?? 0} running
            </div>
          </div>
          <Button variant="secondary" size="sm" onClick={() => void recorders.refetch()}>
            <RotateCcw size={15} />
            새로고침
          </Button>
        </PanelHeader>
        <PanelBody>
          <div className="new-table-wrap">
            <table className="new-table">
              <thead>
                <tr>
                  <th className="px-3 py-2 font-medium">상태</th>
                  <th className="px-3 py-2 font-medium">스트림</th>
                  <th className="px-3 py-2 font-medium">현재 세그먼트</th>
                  <th className="px-3 py-2 font-medium">입력</th>
                </tr>
              </thead>
              <tbody>
                {(recorders.data?.workers ?? []).map((worker) => (
                  <tr key={worker.streamName}>
                    <td className="whitespace-nowrap px-3 py-3">
                      <span className="inline-flex items-center gap-2">
                        <StatusDot status={worker.state} />
                        <span className="text-slate-300">{worker.state}</span>
                      </span>
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 font-semibold text-slate-100">{worker.streamName}</td>
                    <td className="max-w-96 truncate px-3 py-3 font-mono text-xs text-slate-400">
                      {worker.current ?? "-"}
                    </td>
                    <td className="max-w-80 truncate px-3 py-3 font-mono text-xs text-slate-500">{worker.input}</td>
                  </tr>
                ))}
                {!recorders.data?.workers.length && (
                  <tr>
                    <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={4}>
                      실행 중인 녹화 워커가 없습니다.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader className="flex items-center gap-2">
          <Radio size={16} className="text-cyan-300" />
          <h2 className="text-sm font-semibold">세그먼트 정책</h2>
        </PanelHeader>
        <PanelBody className="grid gap-3 md:grid-cols-3">
          <MetricCard label="세그먼트 길이" value={`${recorders.data?.segmentMinutes ?? "-"}분`} />
          <MetricCard label="저장 방식" value="temp -> recordings" />
          <MetricCard label="삭제 대상" value="완료 ready 파일" />
        </PanelBody>
      </Panel>
    </div>
  );
}

function MetricCard({
  icon: Icon,
  label,
  value,
}: {
  icon?: typeof HardDrive;
  label: string;
  value: string;
}) {
  return (
    <div className="new-feature-card flex items-center justify-between gap-3">
      <div>
        <div className="text-xs text-slate-500">{label}</div>
        <div className="mt-1 text-sm font-semibold text-slate-100">{value}</div>
      </div>
      {Icon && (
        <div className="new-feature-icon">
          <Icon size={17} />
        </div>
      )}
    </div>
  );
}

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(value >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function bytesToGB(bytes: number) {
  return bytes / 1024 / 1024 / 1024;
}
