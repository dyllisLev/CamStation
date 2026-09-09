import { useMemo, useState } from "react";
import type { RecordingSegment, RecordingSegmentFilter } from "../app/api";
import { isViewerMode } from "../app/viewerMode";
import { RecordingBrowserWorkspace, type RecordingBrowserRequest } from "../components/playback/RecordingBrowserWorkspace";
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

type RecordingsTab = "playback" | "management";

export function RecordingsPage() {
  return isViewerMode(window.location.search) ? <ViewerRecordingsPage /> : <OperatorRecordingsPage />;
}

function ViewerRecordingsPage() {
  return (
    <div className="h-full min-h-0">
      <RecordingBrowserWorkspace />
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
  const [activeTab, setActiveTab] = useState<RecordingsTab>("playback");
  const [playbackRequest, setPlaybackRequest] = useState<RecordingBrowserRequest>();

  const playSegment = (segment: RecordingSegment) => {
    setPlaybackRequest({
      requestId: `${segment.id}:${Date.now()}`,
      segmentId: segment.id,
      cameraId: segment.camera_id,
      cameraKeyHint: segment.streamName,
      atMs: segment.ts_start * 1000,
    });
    setActiveTab("playback");
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex shrink-0 items-center gap-1 border-b border-slate-800" role="tablist" aria-label="녹화 화면">
        <button
          className={activeTab === "playback" ? "recordings-page-tab recordings-page-tab-active" : "recordings-page-tab"}
          type="button"
          role="tab"
          aria-selected={activeTab === "playback"}
          onClick={() => setActiveTab("playback")}
        >
          영상 재생
        </button>
        <button
          className={activeTab === "management" ? "recordings-page-tab recordings-page-tab-active" : "recordings-page-tab"}
          type="button"
          role="tab"
          aria-selected={activeTab === "management"}
          onClick={() => setActiveTab("management")}
        >
          녹화 관리
        </button>
      </div>

      <div className={activeTab === "playback" ? "min-h-0 flex-1" : "hidden"} role="tabpanel">
        <RecordingBrowserWorkspace active={activeTab === "playback"} request={playbackRequest} />
      </div>

      <div className={activeTab === "management" ? "min-h-0 flex-1 overflow-y-auto pr-1" : "hidden"} role="tabpanel">
        <div className="space-y-4">
          <RecordingStoragePanel storage={storage} cleanup={cleanup} />
          <RecorderWorkersPanel
            recorders={recorders}
            startRecorder={startRecorder}
            stopRecorder={stopRecorder}
            message={workerMessage}
            onMessage={setWorkerMessage}
          />
          <RecordingSegmentsWorkspace onPlaySegment={playSegment} />
        </div>
      </div>
    </div>
  );
}

function RecordingSegmentsWorkspace({ onPlaySegment }: { readonly onPlaySegment: (segment: RecordingSegment) => void }) {
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
    onPlaySegment={onPlaySegment}
    onDeleteSegment={deleteSelectedSegment}
    onCancelDelete={() => setArmedDeleteId(null)}
    onRefresh={() => void segments.refetch()}
  />;
}
