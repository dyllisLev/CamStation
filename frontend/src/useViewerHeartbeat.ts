import { useEffect } from 'react'
import { completeViewerCommand, getPendingViewerCommands, sendViewerHeartbeat } from './api/client'
import type { Camera, ViewerCommand } from './types'
import { buildViewerHeartbeat } from './viewerHealth'

type ViewerAction = ViewerCommand['command']

interface ElectronViewerIdentity {
  clientId: string
  name: string
  appVersion: string
  platform: string
  hostname: string
  pid: number
  startedAt: number
}

interface ElectronApi {
  getViewerIdentity?: () => Promise<ElectronViewerIdentity>
  viewerAction?: (action: ViewerAction) => Promise<{ ok: boolean; message?: string }>
}

declare global {
  interface Window {
    electronAPI?: ElectronApi
  }
}

function fallbackIdentity(): ElectronViewerIdentity {
  const key = 'camstation.viewer.clientId'
  let clientId = window.localStorage.getItem(key)
  if (!clientId) {
    clientId = `browser-${crypto.randomUUID?.() ?? Math.random().toString(36).slice(2)}`
    window.localStorage.setItem(key, clientId)
  }
  return {
    clientId,
    name: '브라우저 뷰어',
    appVersion: 'browser',
    platform: navigator.platform,
    hostname: location.hostname,
    pid: 0,
    startedAt: performance.timeOrigin / 1000,
  }
}

async function getViewerIdentity(): Promise<ElectronViewerIdentity> {
  return window.electronAPI?.getViewerIdentity ? window.electronAPI.getViewerIdentity() : fallbackIdentity()
}

async function runViewerCommand(command: ViewerCommand): Promise<{ ok: boolean; message?: string }> {
  if (command.command === 'ping') return { ok: true, message: 'pong' }
  if (window.electronAPI?.viewerAction) return window.electronAPI.viewerAction(command.command)
  if (command.command === 'reload_page' || command.command === 'refresh_streams') {
    window.location.reload()
    return { ok: true, message: 'page reload requested' }
  }
  return { ok: false, message: 'Electron API unavailable' }
}

export function useViewerHeartbeat(viewerMode: boolean, cameras: Camera[]): void {
  useEffect(() => {
    if (!viewerMode) return
    let cancelled = false
    let identity: ElectronViewerIdentity | null = null

    const tick = async () => {
      try {
        identity ??= await getViewerIdentity()
        if (cancelled) return
        const payload = buildViewerHeartbeat({
          clientId: identity.clientId,
          name: identity.name,
          appVersion: identity.appVersion,
          serverUrl: location.origin,
          platform: identity.platform,
          hostname: identity.hostname,
          pid: identity.pid,
          startedAt: identity.startedAt,
          expectedCameras: cameras.length,
        })
        await sendViewerHeartbeat(payload)
      } catch (error) {
        console.warn('[viewer-heartbeat] failed', error)
      }
    }

    tick()
    const timer = window.setInterval(tick, 10_000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [viewerMode, cameras.length])

  useEffect(() => {
    if (!viewerMode) return
    let cancelled = false
    let identity: ElectronViewerIdentity | null = null

    const poll = async () => {
      try {
        identity ??= await getViewerIdentity()
        if (cancelled) return
        const commands = await getPendingViewerCommands(identity.clientId)
        for (const command of commands) {
          const result = await runViewerCommand(command)
          await completeViewerCommand(identity.clientId, command.id, result)
        }
      } catch (error) {
        console.warn('[viewer-command] failed', error)
      }
    }

    poll()
    const timer = window.setInterval(poll, 5_000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [viewerMode])
}
