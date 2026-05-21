import { describe, expect, it, beforeEach } from 'vitest'
import {
  buildViewerHeartbeat,
  markViewerCameraEvent,
  registerViewerCamera,
  resetViewerHealthStore,
  summarizeViewerHealth,
  unregisterViewerCamera,
} from '../viewerHealth'

beforeEach(() => resetViewerHealthStore())

describe('viewerHealth', () => {
  it('카메라별 수신 상태를 모아 heartbeat payload를 만든다', () => {
    registerViewerCamera('cam1_sub')
    registerViewerCamera('cam2_sub')
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
    registerViewerCamera('cam1_sub')
    registerViewerCamera('cam2_sub')
    markViewerCameraEvent('cam1_sub', { connected: true, videoReadyState: 4, binaryBytes: 1024, videoTime: 10 })
    markViewerCameraEvent('cam2_sub', { connected: true, videoReadyState: 1, binaryBytes: 0, stalledMs: 60000 })

    const summary = summarizeViewerHealth(2)

    expect(summary.expectedCameras).toBe(2)
    expect(summary.healthyCameras).toBe(1)
    expect(summary.state).toBe('degraded')
  })

  it('unmount된 스트림 상태는 heartbeat payload에서 제거한다', () => {
    registerViewerCamera('cam1_sub')
    registerViewerCamera('cam1')
    markViewerCameraEvent('cam1_sub', { connected: true, videoReadyState: 4, binaryBytes: 1024, videoTime: 10 })
    markViewerCameraEvent('cam1', { connected: true, videoReadyState: 4, binaryBytes: 1024, videoTime: 10 })

    unregisterViewerCamera('cam1')

    const payload = buildViewerHeartbeat({
      clientId: 'viewer-1',
      name: '거실 모니터 PC',
      expectedCameras: 1,
    })

    expect(payload.cameras.map(camera => camera.camera_id)).toEqual(['cam1_sub'])
    expect(summarizeViewerHealth(1).healthyCameras).toBe(1)
  })

  it('같은 stream이 중복 mount된 경우 마지막 unmount 전까지 상태를 유지한다', () => {
    registerViewerCamera('cam1_sub')
    registerViewerCamera('cam1_sub')
    markViewerCameraEvent('cam1_sub', { connected: true, videoReadyState: 4, binaryBytes: 1024, videoTime: 10 })

    unregisterViewerCamera('cam1_sub')
    expect(buildViewerHeartbeat({ clientId: 'viewer-1', name: 'viewer', expectedCameras: 1 }).cameras).toHaveLength(1)

    unregisterViewerCamera('cam1_sub')
    expect(buildViewerHeartbeat({ clientId: 'viewer-1', name: 'viewer', expectedCameras: 1 }).cameras).toHaveLength(0)
  })
})
