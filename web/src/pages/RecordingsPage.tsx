import { useMemo, useState } from "react";
import type { RecordingSegmentFilter } from "../app/api";
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
  const storage = useRecordingStorage();
  const recorders = useRecorderStatus();
  const cleanup = useCleanupRecordings();
  const startRecorder = useStartRecorder();
  const stopRecorder = useStopRecorder();
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
  const [workerMessage, setWorkerMessage] = useState("");
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
    for (const worker of recorders.data?.workers ?? []) {
      names.add(worker.streamName);
    }
    for (const segment of segments.data?.segments ?? []) {
      names.add(segment.streamName);
    }
    return Array.from(names).sort((left, right) => left.localeCompare(right));
  }, [recorders.data?.workers, segments.data?.segments]);

  const deleteSelectedSegment = (id: number) => {
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
      <RecordingSegmentsPanel
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
      />
    </div>
  );
}
