import { Activity, EyeOff, ListRestart, RadioTower, RefreshCw, RotateCcw, SearchCheck, ShieldCheck, Users } from "lucide-react";
import type { Camera, StreamRuntime } from "../app/api";
import { useCameras, useProbeStream, useRestartStream, useRestartStreams, useStreamStatus } from "../app/queries";
import { FeatureMatrix } from "../components/FeatureMatrix";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../components/ui/panel";

type StreamRow = {
  readonly streamName: string;
  readonly cameraName: string;
  readonly cameraState: string;
  readonly operationStreamName?: string;
  readonly runtime?: StreamRuntime;
  readonly source: "camera" | "runtime";
};

export function StreamsPage() {
  const cameras = useCameras();
  const streams = useStreamStatus();
  const restartAll = useRestartStreams();
  const restartStream = useRestartStream();
  const probeStream = useProbeStream();

  const runtime = streams.data?.streams ?? {};
  const rows = buildStreamRows(cameras.data ?? [], runtime);
  const configuredCount = (cameras.data ?? []).filter((camera) => camera.streamName !== "").length;
  const runtimeCount = Object.keys(runtime).length;
  const runningCount = rows.filter((row) => row.runtime?.state === "running").length;
  const producerCount = rows.reduce((total, row) => total + (row.runtime?.producerCount ?? 0), 0);
  const consumerCount = rows.reduce((total, row) => total + (row.runtime?.consumerCount ?? 0), 0);
  const viewerCount = rows.reduce((total, row) => total + (row.runtime?.viewerCount ?? 0), 0);
  const missingRuntimeCount = rows.filter((row) => row.source === "camera" && !row.runtime).length;
  const configState = streams.data?.error ? "error" : missingRuntimeCount > 0 ? "degraded" : "running";
  const managerState = streams.data?.running ? "running" : streams.data?.installed ? "offline" : "error";

  return (
    <div className="space-y-4">
      <FeatureMatrix
        title="스트림 운영"
        items={[
          {
            icon: RadioTower,
            title: "런타임",
            status: managerState,
            detail: `${runningCount}/${rows.length} 실행, 프로듀서 ${producerCount}개`,
          },
          {
            icon: Users,
            title: "소비자",
            status: consumerCount > 0 ? "running" : "unknown",
            detail: `소비자 ${consumerCount}개, 뷰어 ${viewerCount}개`,
          },
          {
            icon: ListRestart,
            title: "생성 설정",
            status: configState,
            detail: `카메라 ${configuredCount}개 기준, 런타임 ${runtimeCount}개 확인`,
          },
          {
            icon: ShieldCheck,
            title: "노출 경계",
            status: "running",
            detail: "스트림 제어는 CamStation API 뒤에서만 실행됩니다.",
          },
          {
            icon: EyeOff,
            title: "비밀 보호",
            status: "running",
            detail: "원본 설정과 카메라 접속 정보는 이 화면에 표시하지 않습니다.",
          },
          {
            icon: Activity,
            title: "소유권",
            status: "info",
            detail: "스트림 삭제는 카메라 관리에서만 처리합니다.",
          },
        ]}
      />

      <Panel>
        <PanelHeader className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 className="text-sm font-semibold">스트림 상세</h2>
            <p className="mt-1 text-xs text-slate-500">카메라에서 생성된 안전한 런타임 상태만 표시합니다.</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={() => void streams.refetch()} disabled={streams.isFetching}>
              <RefreshCw className={streams.isFetching ? "animate-spin" : undefined} size={16} />
              새로고침
            </Button>
            <Button type="button" variant="secondary" size="sm" onClick={() => restartAll.mutate()} disabled={restartAll.isPending}>
              <RotateCcw className={restartAll.isPending ? "animate-spin" : undefined} size={16} />
              전체 재시작
            </Button>
          </div>
        </PanelHeader>
        <PanelBody className="space-y-3">
          <OperationNotice restartAll={restartAll} restartStream={restartStream} probeStream={probeStream} />
          {streams.data?.error && <div className="new-form-error">{streams.data.error}</div>}
          <div className="new-table-wrap">
            <table className="new-table new-camera-table">
              <thead>
                <tr>
                  <th className="px-3 py-2 font-medium">스트림</th>
                  <th className="px-3 py-2 font-medium">카메라</th>
                  <th className="px-3 py-2 font-medium">상태</th>
                  <th className="px-3 py-2 font-medium">프로듀서</th>
                  <th className="px-3 py-2 font-medium">소비자</th>
                  <th className="px-3 py-2 font-medium">뷰어</th>
                  <th className="px-3 py-2 font-medium">작업</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.streamName}>
                    <td className="px-3 py-3 font-mono text-xs text-slate-200" data-label="스트림">
                      {row.streamName}
                    </td>
                    <td className="px-3 py-3 text-slate-300" data-label="카메라">
                      {row.cameraName}
                    </td>
                    <td className="px-3 py-3" data-label="상태">
                      <Badge value={row.runtime?.state ?? row.cameraState} />
                    </td>
                    <td className="px-3 py-3 text-slate-300" data-label="프로듀서">
                      {row.runtime?.producerCount ?? 0}
                    </td>
                    <td className="px-3 py-3 text-slate-300" data-label="소비자">
                      {row.runtime?.consumerCount ?? 0}
                    </td>
                    <td className="px-3 py-3 text-slate-300" data-label="뷰어">
                      {row.runtime?.viewerCount ?? 0}
                    </td>
                    <td className="px-3 py-3" data-label="작업">
                      <div className="flex flex-wrap gap-2">
                        <Button
                          type="button"
                          variant="secondary"
                          size="sm"
                          onClick={() => {
                            if (row.operationStreamName) restartStream.mutate(row.operationStreamName);
                          }}
                          disabled={!row.operationStreamName || restartStream.isPending}
                        >
                          <RotateCcw
                            className={restartStream.isPending && restartStream.variables === row.operationStreamName ? "animate-spin" : undefined}
                            size={15}
                          />
                          개별 재시작
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            if (row.operationStreamName) probeStream.mutate(row.operationStreamName);
                          }}
                          disabled={!row.operationStreamName || probeStream.isPending}
                        >
                          <SearchCheck
                            className={probeStream.isPending && probeStream.variables === row.operationStreamName ? "animate-spin" : undefined}
                            size={15}
                          />
                          재탐지
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
                {rows.length === 0 && (
                  <tr>
                    <td colSpan={7} className="px-3 py-10 text-center text-sm text-slate-500">
                      생성된 스트림이 없습니다. 카메라 관리에서 스트림을 등록하면 여기에 표시됩니다.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </PanelBody>
      </Panel>

      <Panel>
        <PanelHeader>
          <h2 className="text-sm font-semibold">삭제 소유권</h2>
        </PanelHeader>
        <PanelBody className="grid gap-2 text-sm text-slate-300 md:grid-cols-3">
          <div className="new-feature-card">
            <div className="text-xs font-semibold text-slate-500">원칙</div>
            <div className="mt-2">스트림은 카메라 설정에서 파생됩니다.</div>
          </div>
          <div className="new-feature-card">
            <div className="text-xs font-semibold text-slate-500">운영 작업</div>
            <div className="mt-2">재시작과 재탐지만 이 화면에서 실행합니다.</div>
          </div>
          <div className="new-feature-card">
            <div className="text-xs font-semibold text-slate-500">삭제 없음</div>
            <div className="mt-2">삭제가 필요하면 카메라 관리에서 소유 리소스를 변경합니다.</div>
          </div>
        </PanelBody>
      </Panel>
    </div>
  );
}

function buildStreamRows(cameras: readonly Camera[], runtime: Readonly<Record<string, StreamRuntime>>): readonly StreamRow[] {
  const cameraRows = cameras.flatMap((camera) => {
    const streamNames = camera.streamOutputs.map((output) => output.streamName).filter((streamName) => streamName !== "");
    const names = streamNames.length > 0 ? streamNames : [camera.streamName].filter((streamName) => streamName !== "");
    return names.map((streamName) => ({
      streamName,
      cameraName: camera.name,
      cameraState: camera.state,
      operationStreamName: camera.streamName,
      runtime: runtime[streamName],
      source: "camera" as const,
    }));
  });
  const cameraStreamNames = new Set(cameraRows.map((row) => row.streamName));
  const runtimeRows = Object.entries(runtime)
    .filter(([streamName]) => !cameraStreamNames.has(streamName))
    .map(([streamName, streamRuntime]) => ({
      streamName,
      cameraName: "카메라 매핑 없음",
      cameraState: "unknown",
      runtime: streamRuntime,
      source: "runtime" as const,
    }));
  return [...cameraRows, ...runtimeRows].sort((left, right) => left.streamName.localeCompare(right.streamName));
}

function OperationNotice({
  restartAll,
  restartStream,
  probeStream,
}: {
  readonly restartAll: ReturnType<typeof useRestartStreams>;
  readonly restartStream: ReturnType<typeof useRestartStream>;
  readonly probeStream: ReturnType<typeof useProbeStream>;
}) {
  if (restartAll.isError) return <div className="new-form-error">전체 스트림 재시작에 실패했습니다. 잠시 후 다시 시도하거나 시스템 상태를 확인하세요.</div>;
  if (restartStream.isError) return <div className="new-form-error">스트림 재시작에 실패했습니다. 잠시 후 다시 시도하거나 시스템 상태를 확인하세요.</div>;
  if (probeStream.isError) return <div className="new-form-error">스트림 재탐지에 실패했습니다. 잠시 후 다시 시도하거나 시스템 상태를 확인하세요.</div>;
  if (restartAll.isPending) return <div className="text-xs text-cyan-200">전체 스트림 재시작 중...</div>;
  if (restartStream.isPending) return <div className="text-xs text-cyan-200">{restartStream.variables} 재시작 중...</div>;
  if (probeStream.isPending) return <div className="text-xs text-cyan-200">{probeStream.variables} 재탐지 중...</div>;
  if (restartStream.isSuccess) return <div className="text-xs text-emerald-200">{restartStream.data.streamName} 재시작 완료</div>;
  if (probeStream.isSuccess) {
    const probe = probeStream.data.probe;
    const result = probe?.reachable ? "도달 가능" : "확인 필요";
    return <div className="text-xs text-emerald-200">{probeStream.data.streamName} 재탐지 완료: {result}</div>;
  }
  if (restartAll.isSuccess) return <div className="text-xs text-emerald-200">전체 스트림 재시작 완료</div>;
  return <div className="text-xs text-slate-500">작업 결과와 이벤트 갱신은 스트림 훅에서 처리합니다.</div>;
}
