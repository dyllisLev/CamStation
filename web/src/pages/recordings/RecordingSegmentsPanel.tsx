import { Download, Eye, Loader2, RotateCcw, Trash2, X } from "lucide-react";
import type { UseQueryResult } from "@tanstack/react-query";
import type { RecordingSegment, RecordingSegmentsResponse } from "../../app/api";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Panel, PanelBody, PanelHeader } from "../../components/ui/panel";
import { BackupStateBadge } from "./RecordingBackupState";
import { DetailItem } from "./RecordingDetailItem";
import { backupStateLabel } from "./recordingBackupStateLabel";
import { formatBytes, formatDuration, formatSegmentTime, safeSegmentUrl, statusLabel } from "./recordingUtils";

const statusOptions: readonly { readonly value: string; readonly label: string }[] = [
  { value: "", label: "전체 상태" },
  { value: "ready", label: "완료" },
  { value: "recording", label: "녹화 중" },
  { value: "finalizing", label: "마무리" },
  { value: "failed", label: "실패" },
  { value: "deleted", label: "삭제됨" },
];

type RecordingSegmentsPanelProps = {
  readonly readOnly?: boolean;
  readonly segments: UseQueryResult<RecordingSegmentsResponse, Error>;
  readonly selectedSegment: UseQueryResult<RecordingSegment, Error>;
  readonly streamOptions: readonly string[];
  readonly streamFilter: string;
  readonly statusFilter: string;
  readonly fromFilter: string;
  readonly toFilter: string;
  readonly limitFilter: number;
  readonly armedDeleteId: number | null;
  readonly deletePending: boolean;
  readonly deleteError: string;
  readonly deleteSuccess: string;
  readonly selectedSegmentId: number | null;
  readonly onStreamFilterChange: (value: string) => void;
  readonly onStatusFilterChange: (value: string) => void;
  readonly onFromFilterChange: (value: string) => void;
  readonly onToFilterChange: (value: string) => void;
  readonly onLimitFilterChange: (value: number) => void;
  readonly onSelectSegment: (id: number | null) => void;
  readonly onDeleteSegment: (id: number) => void;
  readonly onCancelDelete: () => void;
  readonly onRefresh: () => void;
};

export function RecordingSegmentsPanel(props: RecordingSegmentsPanelProps) {
  const rows = props.segments.data?.segments ?? [];
  const detail = props.selectedSegment.data;

  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
      <Panel>
        <PanelHeader className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-sm font-semibold">녹화 세그먼트</h2>
            <div className="mt-1 text-xs text-slate-500">{rows.length}개 표시</div>
          </div>
          <Button variant="secondary" size="sm" onClick={props.onRefresh}>
            <RotateCcw size={15} />
            새로고침
          </Button>
        </PanelHeader>
        <PanelBody className="space-y-3">
          <SegmentFilters {...props} />
          {props.segments.isLoading && <div className="text-sm text-slate-400">녹화 세그먼트를 불러오는 중입니다.</div>}
          {props.segments.error && <div className="text-sm text-red-300">녹화 세그먼트를 불러오지 못했습니다: {props.segments.error.message}</div>}
          {props.deleteError && <div className="text-sm text-red-300">삭제 실패: {props.deleteError}</div>}
          {props.deleteSuccess && <div className="text-sm text-emerald-300">{props.deleteSuccess}</div>}
          <div className="new-table-wrap overflow-x-auto">
            <table className="new-table new-camera-table min-w-[1180px]">
              <thead>
                <tr>
                  <th className="px-3 py-2 font-medium">상태</th>
                  <th className="px-3 py-2 font-medium">백업</th>
                  <th className="px-3 py-2 font-medium">스트림</th>
                  <th className="px-3 py-2 font-medium">시작</th>
                  <th className="px-3 py-2 font-medium">길이</th>
                  <th className="px-3 py-2 font-medium">크기</th>
                  <th className="px-3 py-2 font-medium">파일</th>
                  <th className="px-3 py-2 font-medium">작업</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((segment) => (
                  <SegmentRow
                    key={segment.id}
                    segment={segment}
                    selected={props.selectedSegmentId === segment.id}
                    armedDeleteId={props.armedDeleteId}
                    deletePending={props.deletePending}
                    readOnly={props.readOnly}
                    onSelectSegment={props.onSelectSegment}
                    onDeleteSegment={props.onDeleteSegment}
                    onCancelDelete={props.onCancelDelete}
                  />
                ))}
                {!rows.length && !props.segments.isLoading && (
                  <tr>
                    <td className="px-3 py-8 text-center text-sm text-slate-500" colSpan={8}>조건에 맞는 녹화 세그먼트가 없습니다.</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </PanelBody>
      </Panel>
      <Panel>
        <PanelHeader className="flex items-center justify-between gap-3">
          <h2 className="text-sm font-semibold">세그먼트 상세</h2>
          {props.selectedSegmentId && (
            <Button variant="ghost" size="sm" onClick={() => props.onSelectSegment(null)} aria-label="상세 닫기">
              <X size={15} />
            </Button>
          )}
        </PanelHeader>
        <PanelBody>
          {!props.selectedSegmentId && <div className="text-sm text-slate-500">세그먼트를 선택하면 상세 정보와 재생/다운로드 작업이 표시됩니다.</div>}
          {props.selectedSegment.isLoading && <div className="text-sm text-slate-400">상세 정보를 불러오는 중입니다.</div>}
          {props.selectedSegment.error && <div className="text-sm text-red-300">상세 정보를 불러오지 못했습니다: {props.selectedSegment.error.message}</div>}
          {detail && <SegmentDetail segment={detail} readOnly={props.readOnly} />}
        </PanelBody>
      </Panel>
    </div>
  );
}

function SegmentFilters(props: RecordingSegmentsPanelProps) {
  return (
    <div className="grid gap-2 md:grid-cols-5">
      <label className="space-y-1">
        <span className="text-xs font-medium text-slate-400">스트림</span>
        <select className="new-form-control" value={props.streamFilter} onChange={(event) => props.onStreamFilterChange(event.target.value)}>
          <option value="">전체 스트림</option>
          {props.streamOptions.map((streamName) => <option key={streamName} value={streamName}>{streamName}</option>)}
        </select>
      </label>
      <label className="space-y-1">
        <span className="text-xs font-medium text-slate-400">상태</span>
        <select className="new-form-control" value={props.statusFilter} onChange={(event) => props.onStatusFilterChange(event.target.value)}>
          {statusOptions.map((option) => <option key={option.value || "all"} value={option.value}>{option.label}</option>)}
        </select>
      </label>
      <label className="space-y-1">
        <span className="text-xs font-medium text-slate-400">시작 이후</span>
        <input className="new-form-control" type="datetime-local" value={props.fromFilter} onChange={(event) => props.onFromFilterChange(event.target.value)} />
      </label>
      <label className="space-y-1">
        <span className="text-xs font-medium text-slate-400">시작 이전</span>
        <input className="new-form-control" type="datetime-local" value={props.toFilter} onChange={(event) => props.onToFilterChange(event.target.value)} />
      </label>
      <label className="space-y-1">
        <span className="text-xs font-medium text-slate-400">표시 수</span>
        <select className="new-form-control" value={props.limitFilter} onChange={(event) => props.onLimitFilterChange(Number(event.target.value))}>
          <option value={100}>100</option>
          <option value={200}>200</option>
          <option value={500}>500</option>
          <option value={1000}>1000</option>
        </select>
      </label>
    </div>
  );
}

function SegmentRow({ segment, selected, armedDeleteId, deletePending, readOnly, onSelectSegment, onDeleteSegment, onCancelDelete }: {
  readonly segment: RecordingSegment;
  readonly selected: boolean;
  readonly armedDeleteId: number | null;
  readonly deletePending: boolean;
  readonly readOnly?: boolean;
  readonly onSelectSegment: (id: number) => void;
  readonly onDeleteSegment: (id: number) => void;
  readonly onCancelDelete: () => void;
}) {
  const playHref = safeSegmentUrl(segment.playUrl, segment.id, "play");
  const downloadHref = safeSegmentUrl(segment.downloadUrl, segment.id, "download");
  const armed = armedDeleteId === segment.id;
  return (
    <tr className={selected ? "bg-slate-900/70" : undefined} aria-selected={selected}>
      <td className="whitespace-nowrap px-3 py-3" data-label="상태"><Badge value={segment.status} /></td>
      <td className="whitespace-nowrap px-3 py-3" data-label="백업"><BackupStateBadge segment={segment} /></td>
      <td className="whitespace-nowrap px-3 py-3 font-semibold text-slate-100" data-label="스트림">{segment.streamName}</td>
      <td className="whitespace-nowrap px-3 py-3 font-mono text-xs text-slate-400" data-label="시작">{formatSegmentTime(segment.ts_start)}</td>
      <td className="whitespace-nowrap px-3 py-3 text-slate-300" data-label="길이">{formatDuration(segment.ts_start, segment.ts_end)}</td>
      <td className="whitespace-nowrap px-3 py-3 text-slate-300" data-label="크기">{formatBytes(segment.file_size)}</td>
      <td className="max-w-72 truncate px-3 py-3 font-mono text-xs text-slate-500" data-label="파일">{segment.filename}</td>
      <td className="min-w-[21rem] px-3 py-3" data-label="작업">
        <div className="flex flex-nowrap gap-2">
          <Button variant="secondary" size="sm" onClick={() => onSelectSegment(segment.id)}><Eye size={14} />{readOnly ? "재생" : "상세"}</Button>
          {!readOnly && (playHref ? (
            <Button asChild variant="secondary" size="sm">
              <a href={playHref} target="_blank" rel="noreferrer"><Eye size={14} />재생</a>
            </Button>
          ) : (
            <Button variant="secondary" size="sm" disabled><Eye size={14} />재생</Button>
          ))}
          {!readOnly && (downloadHref ? (
            <Button asChild variant="secondary" size="sm">
              <a href={downloadHref} download><Download size={14} />다운로드</a>
            </Button>
          ) : (
            <Button variant="secondary" size="sm" disabled><Download size={14} />다운로드</Button>
          ))}
          {!readOnly && <Button variant={armed ? "danger" : "secondary"} size="sm" disabled={deletePending} onClick={() => onDeleteSegment(segment.id)}>
            {deletePending && armed ? <Loader2 className="animate-spin" size={14} /> : <Trash2 size={14} />}
            {armed ? "삭제 확인" : "삭제"}
          </Button>}
          {!readOnly && armed && <Button variant="ghost" size="sm" disabled={deletePending} onClick={onCancelDelete}>취소</Button>}
        </div>
      </td>
    </tr>
  );
}

function SegmentDetail({ segment, readOnly }: { readonly segment: RecordingSegment; readonly readOnly?: boolean }) {
  const playHref = safeSegmentUrl(segment.playUrl, segment.id, "play");
  const downloadHref = safeSegmentUrl(segment.downloadUrl, segment.id, "download");
  return (
    <div className="space-y-3">
      {playHref ? <video className="aspect-video w-full rounded-[7px] border border-slate-800 bg-black" controls src={playHref} /> : <div className="rounded-[7px] border border-slate-800 bg-slate-950 p-4 text-sm text-slate-500">완료된 세그먼트만 재생할 수 있습니다.</div>}
      <div className="grid gap-2 text-sm">
        <DetailItem label="상태" value={statusLabel(segment.status)} />
        <DetailItem label="백업" value={backupStateLabel(segment)} />
        <DetailItem label="스트림" value={segment.streamName} />
        <DetailItem label="파일" value={segment.filename} mono />
        <DetailItem label="시작" value={formatSegmentTime(segment.ts_start)} />
        <DetailItem label="종료" value={formatSegmentTime(segment.ts_end)} />
        <DetailItem label="크기" value={formatBytes(segment.file_size)} />
      </div>
      {!readOnly && <div className="flex flex-wrap gap-2">
        {playHref ? (
          <Button asChild variant="primary">
            <a href={playHref} target="_blank" rel="noreferrer"><Eye size={16} />재생 열기</a>
          </Button>
        ) : (
          <Button variant="primary" disabled><Eye size={16} />재생 열기</Button>
        )}
        {downloadHref ? (
          <Button asChild variant="secondary">
            <a href={downloadHref} download><Download size={16} />다운로드</a>
          </Button>
        ) : (
          <Button variant="secondary" disabled><Download size={16} />다운로드</Button>
        )}
      </div>}
    </div>
  );
}
