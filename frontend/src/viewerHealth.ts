export interface ViewerCameraEvent {
  connected?: boolean
  videoReadyState?: number
  binaryBytes?: number
  videoTime?: number
  stalledMs?: number
  reconnectCount?: number
  error?: string | null
}

export interface ViewerCameraHeartbeat {
  camera_id: string
  connected: boolean
  video_ready_state: number
  last_binary_at: number | null
  last_video_time: number | null
  last_video_time_at: number | null
  stalled_ms: number
  reconnect_count: number
  error: string | null
}

export interface ViewerHeartbeatIdentity {
  clientId: string
  name: string
  appVersion?: string | null
  serverUrl?: string | null
  platform?: string | null
  hostname?: string | null
  pid?: number | null
  startedAt?: number | null
  expectedCameras?: number
}

export interface ViewerHeartbeatPayload {
  client_id: string
  name: string
  app_version?: string | null
  server_url?: string | null
  platform?: string | null
  hostname?: string | null
  pid?: number | null
  started_at?: number | null
  expected_cameras: number
  cameras: ViewerCameraHeartbeat[]
}

export interface ViewerHealthSummary {
  expectedCameras: number
  healthyCameras: number
  state: 'healthy' | 'degraded' | 'unknown'
}

interface InternalCameraState extends ViewerCameraHeartbeat {
  bytes_received: number
}

const cameraStates = new Map<string, InternalCameraState>()
const activeCameraRefs = new Map<string, number>()

function nowSeconds(): number {
  return Date.now() / 1000
}

function isCameraHealthy(camera: ViewerCameraHeartbeat): boolean {
  return (
    camera.connected &&
    camera.video_ready_state >= 2 &&
    !camera.error &&
    camera.stalled_ms < 30_000 &&
    (camera.last_binary_at !== null || camera.last_video_time_at !== null)
  )
}

export function resetViewerHealthStore(): void {
  cameraStates.clear()
  activeCameraRefs.clear()
}

export function registerViewerCamera(cameraId: string): void {
  activeCameraRefs.set(cameraId, (activeCameraRefs.get(cameraId) ?? 0) + 1)
}

export function unregisterViewerCamera(cameraId: string): void {
  const next = (activeCameraRefs.get(cameraId) ?? 0) - 1
  if (next > 0) {
    activeCameraRefs.set(cameraId, next)
    return
  }
  activeCameraRefs.delete(cameraId)
  cameraStates.delete(cameraId)
}

export function markViewerCameraEvent(cameraId: string, event: ViewerCameraEvent): ViewerCameraHeartbeat {
  const existing = cameraStates.get(cameraId)
  const timestamp = nowSeconds()
  const next: InternalCameraState = existing ?? {
    camera_id: cameraId,
    connected: false,
    video_ready_state: 0,
    last_binary_at: null,
    last_video_time: null,
    last_video_time_at: null,
    stalled_ms: 0,
    reconnect_count: 0,
    error: null,
    bytes_received: 0,
  }

  if (event.connected !== undefined) next.connected = event.connected
  if (event.videoReadyState !== undefined) next.video_ready_state = event.videoReadyState
  if (event.stalledMs !== undefined) next.stalled_ms = event.stalledMs
  if (event.reconnectCount !== undefined) next.reconnect_count = event.reconnectCount
  if (event.error !== undefined) next.error = event.error
  if (event.binaryBytes !== undefined && event.binaryBytes > 0) {
    next.bytes_received += event.binaryBytes
    next.last_binary_at = timestamp
  }
  if (event.videoTime !== undefined) {
    if (next.last_video_time === null || event.videoTime !== next.last_video_time) {
      next.last_video_time_at = timestamp
    }
    next.last_video_time = event.videoTime
  }

  cameraStates.set(cameraId, next)
  return { ...next }
}

export function summarizeViewerHealth(expectedCameras = cameraStates.size): ViewerHealthSummary {
  const cameras = [...cameraStates.values()].filter(camera => activeCameraRefs.has(camera.camera_id))
  const healthyCameras = cameras.filter(isCameraHealthy).length
  return {
    expectedCameras,
    healthyCameras,
    state: expectedCameras <= 0 ? 'unknown' : healthyCameras >= expectedCameras ? 'healthy' : 'degraded',
  }
}

export function buildViewerHeartbeat(identity: ViewerHeartbeatIdentity): ViewerHeartbeatPayload {
  const cameras = [...cameraStates.values()]
    .filter(camera => activeCameraRefs.has(camera.camera_id))
    .sort((a, b) => a.camera_id.localeCompare(b.camera_id))
    .map(({ bytes_received: _bytesReceived, ...camera }) => ({ ...camera }))

  return {
    client_id: identity.clientId,
    name: identity.name,
    app_version: identity.appVersion,
    server_url: identity.serverUrl,
    platform: identity.platform,
    hostname: identity.hostname,
    pid: identity.pid,
    started_at: identity.startedAt,
    expected_cameras: identity.expectedCameras ?? cameras.length,
    cameras,
  }
}
