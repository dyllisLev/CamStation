import { useMemo, useState } from "react";
import type { RecordingSegmentFilter } from "../app/api";
import { isViewerMode } from "../app/viewerMode";
import {
  useCleanupRecordings,
  useDeleteRecordingSegment,
  useRecorderStatus,
  useRecordingSegment,
  useRecordingSegments,
  useRecordingStorage,
  useStartRecorder,
  useStopRecorder,
} from "../app/queries";
import { RecorderWorkersPanel } from "./recordings/RecorderWorkersPanel";
import { RecordingSegmentsPanel } from "./recordings/RecordingSegmentsPanel";
import { RecordingStoragePanel } from "./recordings/RecordingStoragePanel";
import { toSegmentTimeFilter } from "./recordings/recordingUtils";

export function RecordingsPage() {
  return isViewerMode(window.location.search) ? <ViewerRecordingsPage /> : <OperatorRecordingsPage />;
}

function ViewerRecordingsPage() {
  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-lg font-semibold text-slate-100">녹화 영상</h1>
        <p className="mt-1 text-sm text-slate-400">세그먼트를 선택해 영상을 확인하세요.</p>
      </div>
      <RecordingSegmentsWorkspace readOnly />
    </div>
  );
}

function OperatorRecordingsPage() {
  const storage = useRecordingStorage();
  const recorders = useRecorderStatus();
  const cleanup = useCleanupRecordings();
  const startRecorder = useStartRecorder();
  const stopRecorder = useStopRecorder();
  const [workerMessage, setWorkerMessage] = useState("");

  return (
    <div className="space-y-4">
      <RecordingStoragePanel storage={storage} cleanup={cleanup} />
      <RecorderWorkersPanel
        recorders={recorders}
        startRecorder={startRecorder}
        stopRecorder={stopRecorder}
        message={workerMessage}
        onMessage={setWorkerMessage}
      />
      <RecordingSegmentsWorkspace />
    </div>
  );
}

function RecordingSegmentsWorkspace({ readOnly = false }: { readonly readOnly?: boolean }) {
  const recorders = useRecorderStatus();
  const deleteSegment = useDeleteRecordingSegment();
  const [streamFilter, setStreamFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [fromFilter, setFromFilter] = useState("");
  const [toFilter, setToFilter] = useState("");
  const [limitFilter, setLimitFilter] = useState(200);
  const [selectedSegmentId, setSelectedSegmentId] = useState<number | null>(null);
  const [armedDeleteId, setArmedDeleteId] = useState<number | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const [deleteSuccess, setDeleteSuccess] = useState("");
  const selectedSegment = useRecordingSegment(selectedSegmentId ?? 0);

  const segmentFilter = useMemo<RecordingSegmentFilter>(
    () => ({
      stream: streamFilter || undefined,
      status: statusFilter ? [statusFilter] : undefined,
      from: toSegmentTimeFilter(fromFilter),
      to: toSegmentTimeFilter(toFilter),
      limit: limitFilter,
    }),
    [fromFilter, limitFilter, statusFilter, streamFilter, toFilter],
  );
  const segments = useRecordingSegments(segmentFilter);

  const streamOptions = useMemo(() => {
    const names = new Set<string>();
    for (const worker of recorders.data?.workers ?? []) names.add(worker.streamName);
    for (const segment of segments.data?.segments ?? []) names.add(segment.streamName);
    return Array.from(names).sort((left, right) => left.localeCompare(right));
  }, [recorders.data?.workers, segments.data?.segments]);

  const deleteSelectedSegment = (id: number) => {
    if (readOnly) return;
    if (armedDeleteId !== id) {
      setArmedDeleteId(id);
      setDeleteError("");
      setDeleteSuccess("");
      return;
    }
    deleteSegment.mutate(id, {
      onError: (error) => {
        setDeleteError(error instanceof Error ? error.message : "삭제에 실패했습니다.");
        setDeleteSuccess("");
      },
      onSuccess: () => {
        setArmedDeleteId(null);
        setSelectedSegmentId((current) => (current === id ? null : current));
        setDeleteError("");
        setDeleteSuccess("녹화 세그먼트를 삭제했습니다.");
      },
    });
  };

  return <RecordingSegmentsPanel
    readOnly={readOnly}
    segments={segments}
    selectedSegment={selectedSegment}
    streamOptions={streamOptions}
    streamFilter={streamFilter}
    statusFilter={statusFilter}
    fromFilter={fromFilter}
    toFilter={toFilter}
    limitFilter={limitFilter}
    armedDeleteId={armedDeleteId}
    deletePending={deleteSegment.isPending}
    deleteError={deleteError}
    deleteSuccess={deleteSuccess}
    selectedSegmentId={selectedSegmentId}
    onStreamFilterChange={setStreamFilter}
    onStatusFilterChange={setStatusFilter}
    onFromFilterChange={setFromFilter}
    onToFilterChange={setToFilter}
    onLimitFilterChange={setLimitFilter}
    onSelectSegment={setSelectedSegmentId}
    onDeleteSegment={deleteSelectedSegment}
    onCancelDelete={() => setArmedDeleteId(null)}
    onRefresh={() => void segments.refetch()}
  />;
}
