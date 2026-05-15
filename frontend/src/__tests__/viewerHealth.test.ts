import { describe, expect, it, beforeEach } from 'vitest'
import {
  buildViewerHeartbeat,
  markViewerCameraEvent,
  resetViewerHealthStore,
  summarizeViewerHealth,
} from '../viewerHealth'

beforeEach(() => resetViewerHealthStore())

describe('viewerHealth', () => {
  it('카메라별 수신 상태를 모아 heartbeat payload를 만든다', () => {
    markViewerCameraEvent('cam1_sub', { connected: true, videoReadyState: 4, binaryBytes: 1024, videoTime: 10 })
    markViewerCameraEvent('cam2_sub', { connected: false, videoReadyState: 0, error: 'stalled', stalledMs: 45000 })

    const payload = buildViewerHeartbeat({
      clientId: 'viewer-1',
      name: '거실 모니터 PC',
      appVersion: '1.0.2',
      serverUrl: 'http://10.0.0.26',
      platform: 'win32',
      hostname: 'living-room',
      pid: 1234,
      expectedCameras: 2,
    })

    expect(payload.client_id).toBe('viewer-1')
    expect(payload.expected_cameras).toBe(2)
    expect(payload.cameras).toHaveLength(2)
    expect(payload.cameras[0].camera_id).toBe('cam1_sub')
    expect(payload.cameras[0].connected).toBe(true)
    expect(payload.cameras[0].video_ready_state).toBe(4)
    expect(payload.cameras[1].error).toBe('stalled')
  })

  it('정상 수신 카메라 수를 계산한다', () => {
    markViewerCameraEvent('cam1_sub', { connected: true, videoReadyState: 4, binaryBytes: 1024, videoTime: 10 })
    markViewerCameraEvent('cam2_sub', { connected: true, videoReadyState: 1, binaryBytes: 0, stalledMs: 60000 })

    const summary = summarizeViewerHealth(2)

    expect(summary.expectedCameras).toBe(2)
    expect(summary.healthyCameras).toBe(1)
    expect(summary.state).toBe('degraded')
  })
})
