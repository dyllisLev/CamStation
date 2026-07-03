import { Loader2, Play, RotateCcw, Square } from "lucide-react";
import type { UseMutationResult, UseQueryResult } from "@tanstack/react-query";
import type { RecorderControlInput, RecorderStatus } from "../../app/api";
import { StatusDot } from "../../components/StatusDot";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";

type RecorderWorkersPanelProps = {
  readonly recorders: UseQueryResult<RecorderStatus, Error>;
  readonly startRecorder: UseMutationResult<RecorderStatus, Error, RecorderControlInput | undefined>;
  readonly stopRecorder: UseMutationResult<RecorderStatus, Error, RecorderControlInput | undefined>;
  readonly message: string;
  readonly onMessage: (message: string) => void;
};

export function RecorderWorkersPanel({ recorders, startRecorder, stopRecorder, message, onMessage }: RecorderWorkersPanelProps) {
  const workers = recorders.data?.workers ?? [];
  const activeWorkers = workers.filter((worker) => worker.state === "running").length;
  const controlPending = startRecorder.isPending || stopRecorder.isPending;

  const start = (stream?: string) => {
    startRecorder.mutate(
      stream ? { stream } : undefined,
      {
        onError: (error) => onMessage(`녹화 시작 실패: ${error.message}`),
        onSuccess: () => onMessage(stream ? `${stream} 녹화를 시작했습니다.` : "전체 녹화를 시작했습니다."),
      },
    );
  };

  const stop = (stream?: string) => {
    stopRecorder.mutate(
      stream ? { stream } : undefined,
      {
        onError: (error) => onMessage(`녹화 중지 실패: ${error.message}`),
        onSuccess: () => onMessage(stream ? `${stream} 녹화를 중지했습니다.` : "전체 녹화를 중지했습니다."),
      },
    );
  };

  return (
    <Panel>
      <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">녹화 워커</h2>
          <div className="mt-1 text-xs text-slate-500">{activeWorkers} / {workers.length} running</div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" size="sm" disabled={controlPending} onClick={() => start()}>
            {startRecorder.isPending ? <Loader2 className="animate-spin" size={15} /> : <Play size={15} />}
            전체 시작
          </Button>
          <Button variant="secondary" size="sm" disabled={controlPending} onClick={() => stop()}>
            {stopRecorder.isPending ? <Loader2 className="animate-spin" size={15} /> : <Square size={15} />}
            전체 중지
          </Button>
          <Button variant="secondary" size="sm" onClick={() => void recorders.refetch()}>
            <RotateCcw size={15} />
            새로고침
          </Button>
        </div>
      </PanelHeader>
      <PanelBody className="space-y-3">
        {recorders.isLoading && <div className="text-sm text-slate-400">녹화 워커를 불러오는 중입니다.</div>}
        {recorders.error && <div className="text-sm text-red-300">녹화 워커 상태를 불러오지 못했습니다: {recorders.error.message}</div>}
        {message && <div className="text-xs text-cyan-200">{message}</div>}
        <div className="new-table-wrap overflow-x-auto">
          <table className="new-table new-camera-table min-w-[760px]">
            <thead>
              <tr>
                <th className="px-3 py-2 font-medium">상태</th>
                <th className="px-3 py-2 font-medium">스트림</th>
                <th className="px-3 py-2 font-medium">카메라</th>
                <th className="px-3 py-2 font-medium">활동</th>
                <th className="px-3 py-2 font-medium">오류</th>
                <th className="px-3 py-2 font-medium">제어</th>
              </tr>
            </thead>
            <tbody>
              {workers.map((worker) => (
                <tr key={worker.streamName}>
                  <td className="whitespace-nowrap px-3 py-3" data-label="상태">
                    <span className="inline-flex items-center gap-2">
                      <StatusDot status={worker.state} />
                      <Badge value={worker.state} />
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-3 py-3 font-semibold text-slate-100" data-label="스트림">{worker.streamName}</td>
                  <td className="whitespace-nowrap px-3 py-3 font-mono text-xs text-slate-400" data-label="카메라">#{worker.camera_id}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-300" data-label="활동">{worker.state === "running" ? "세그먼트 기록 중" : "대기"}</td>
                  <td className="whitespace-nowrap px-3 py-3 text-slate-400" data-label="오류">{worker.lastError ? "오류 있음" : "-"}</td>
                  <td className="px-3 py-3" data-label="제어">
                    <div className="flex flex-wrap gap-2">
                      <Button variant="secondary" size="sm" disabled={controlPending || worker.state === "running"} onClick={() => start(worker.streamName)}>
                        <Play size={14} />
                        시작
                      </Button>
                      <Button variant="secondary" size="sm" disabled={controlPending || worker.state !== "running"} onClick={() => stop(worker.streamName)}>
                        <Square size={14} />
                        중지
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
              {!workers.length && (
                <tr>
                  <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={6}>등록된 녹화 워커가 없습니다.</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </PanelBody>
    </Panel>
  );
}
