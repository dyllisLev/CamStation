export type PlaybackCamera = {
  cameraKey: string;
  name: string;
  registered: boolean;
  hasRecordings: boolean;
};

export type PlaybackCoverage = {
  startMs: number;
  endMs: number;
  mediaId: string;
  segmentId?: number;
  state: string;
};

export type PlaybackSegment = {
  segmentId: number;
  mediaId: string;
  startMs: number;
  endMs: number;
  state: string;
};

export type PlaybackSegmentsResponse = {
  segments: PlaybackSegment[];
  nextCursor: string | null;
};

export type PlaybackTimelineResponse = {
  cameraKey: string;
  fromMs: number;
  toMs: number;
  coverage: PlaybackCoverage[];
  playableEndMs: number | null;
  recordingActive: boolean;
};

export type PlaybackMedia = {
  id: string;
  kind: "file" | "hls";
  url: string;
  startMs: number;
  endMs: number;
  mediaStartSeconds: number;
  requestedOffsetSeconds: number;
  growing: boolean;
  timeBasis: string;
};

export type PlaybackResolveResponse = {
  status: "found" | "gap" | "edge_wait" | "unsupported";
  requestedAtMs: number;
  previousAtMs?: number;
  nextAtMs?: number;
  media?: PlaybackMedia;
};
