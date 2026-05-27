import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { CSSProperties, MouseEvent, ReactNode } from 'react'
import GridLayout from 'react-grid-layout'
import type { Layout } from 'react-grid-layout'
import { format } from 'date-fns'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import { WebRTCPlayer } from '../../components/WebRTCPlayer'
import { useAllTimelines } from '../../hooks/useAllTimelines'
import { useCameras } from '../../hooks/useCameras'
import { useLayouts } from '../../hooks/useLayouts'
import { useViewerHeartbeat } from '../../useViewerHeartbeat'
import {
  getSettings,
  getStorageStats,
  getSystemVersion,
  getCameraAdmin,
  getViewerVersion,
  listRecordings,
  rebootCamera,
  triggerUpdate,
  setCameraAdminEnabled,
  archiveCameraAdmin,
  applyCameraAdminConfig,
  createCameraAdmin,
  updateCameraAdmin,
  updateSettings,
} from '../../api/client'
import type { Camera, CameraAdminItem, CameraAdminUpdateRequest, RecordingSegment, Settings, StorageStats, SystemVersion, TimelineData } from '../../types'
import {
  NEW_LIVE_TIMELINE_COLLAPSED_KEY,
  calculateLiveGridRowHeight,
  clampLayoutToGridBounds,
  filterRecordingSegments,
  formatClock,
  formatDuration,
  formatStorageSize,
  getBoundedLayoutOrFallback,
  getTimelineToggleLabel,
  isNewViewerMode,
  mergeTimelineRanges,
  readNewLiveTimelineCollapsedPreference,
  recordingUrl,
  shouldRestrictNewViewerPage,
} from './newUiUtils'
import type { NewUiPage, RecordingFilter, TimelineRange } from './newUiUtils'
import './newCamStation.css'

type NewPage = NewUiPage

type Navigate = (page: NewPage) => void

type TimelineMotion = { ts_start: number; ts_end: number | null }
type CameraEditForm = {
  display_name: string
  location: string
  notes: string
  main_stream_url: string
  sub_stream_url: string
  onvif_host: string
  onvif_port: string
  onvif_username: string
  onvif_password: string
}

const GRID_COLS = 48
const LIVE_GRID_MAX_ROWS = 48
const LIVE_GRID_MARGIN: [number, number] = [4, 4]
const RESIZE_HANDLE_SIZE = 14
const RESIZE_EDGE_SIZE = 8

function initialPageFromPath(pathname = window.location.pathname, viewerMode = isNewViewerMode(window.location.search)): NewPage {
  if (viewerMode) return 'live'
  if (pathname.startsWith('/new/recordings')) return 'recordings'
  if (pathname.startsWith('/new/settings')) return 'settings'
  return 'live'
}

function pageToPath(page: NewPage): string {
  if (page === 'live') return '/new'
  return `/new/${page}`
}

function ResizeHandle({ handleAxis = 'se', ...rest }: { handleAxis?: string }) {
  const edge = RESIZE_EDGE_SIZE
  const corner = RESIZE_HANDLE_SIZE
  const base: CSSProperties = { position: 'absolute', zIndex: 10 }
  const styleByAxis: Record<string, CSSProperties> = {
    n: { ...base, top: 0, left: corner, right: corner, height: edge, cursor: 'ns-resize' },
    s: { ...base, bottom: 0, left: corner, right: corner, height: edge, cursor: 'ns-resize' },
    e: { ...base, right: 0, top: corner, bottom: corner, width: edge, cursor: 'ew-resize' },
    w: { ...base, left: 0, top: corner, bottom: corner, width: edge, cursor: 'ew-resize' },
    ne: { ...base, top: 0, right: 0, width: corner, height: corner, cursor: 'ne-resize' },
    nw: { ...base, top: 0, left: 0, width: corner, height: corner, cursor: 'nw-resize' },
    se: { ...base, bottom: 0, right: 0, width: corner, height: corner, cursor: 'se-resize' },
    sw: { ...base, bottom: 0, left: 0, width: corner, height: corner, cursor: 'sw-resize' },
  }
  return <div {...rest} style={styleByAxis[handleAxis] ?? base} className={`new-resize-handle new-resize-handle-${handleAxis}`} />
}

function NewHeader({
  page,
  onNavigate,
  viewerMode = false,
  children,
}: {
  page: NewPage
  onNavigate: Navigate
  viewerMode?: boolean
  children?: ReactNode
}) {
  const navigate = (next: NewPage) => {
    if (shouldRestrictNewViewerPage(next, viewerMode)) return
    onNavigate(next)
    const nextPath = pageToPath(next)
    window.history.pushState(null, '', viewerMode ? `${nextPath}?viewer=1` : nextPath)
  }

  return (
    <header className="new-command">
      <div className="new-brand" aria-label="CamStation 신규 UI">
        <div className="new-brand-mark">CS</div>
        <div>
          <div className="new-brand-title">CamStation</div>
          <div className="new-mini">HOME MONITOR</div>
        </div>
      </div>
      <nav className="new-nav" aria-label="신규 UI 주요 화면">
        <button className={page === 'live' ? 'new-active' : ''} onClick={() => navigate('live')}>라이브</button>
        {!viewerMode && (
          <>
            <button className={page === 'recordings' ? 'new-active' : ''} onClick={() => navigate('recordings')}>녹화</button>
            <button className={page === 'settings' ? 'new-active' : ''} onClick={() => navigate('settings')}>설정</button>
          </>
        )}
      </nav>
      {children}
    </header>
  )
}

function NewCameraTile({
  camera,
  hasMotion,
  selected,
  onSelect,
  onFocus,
}: {
  camera: Camera
  hasMotion: boolean
  selected: boolean
  onSelect: () => void
  onFocus: () => void
}) {
  const streamId = camera.has_sub ? `${camera.id}_sub` : camera.id
  return (
    <article className={`new-camera-tile ${selected ? 'new-selected' : ''} ${camera.online ? '' : 'new-offline'}`} onClick={onSelect}>
      <WebRTCPlayer camId={streamId} style={{ background: '#05070a' }} />
      <div className="new-tile-head cam-drag-handle">
        <span className="new-state" />
        <strong>{camera.name}</strong>
        <span className="new-cam-id">{camera.id}</span>
      </div>
      <button className="new-focus-btn" type="button" onClick={(event) => { event.stopPropagation(); onFocus() }}>
        집중 보기
      </button>
      {hasMotion && <span className="new-motion-tag">MOTION</span>}
    </article>
  )
}

function NewCameraGrid({
  cameras,
  layout,
  motionCams,
  selectedCamId,
  onLayoutChange,
  onSelectCamera,
  onFocusCamera,
  readOnly = false,
}: {
  cameras: Camera[]
  layout: Layout[]
  motionCams: Set<string>
  selectedCamId: string
  onLayoutChange: (layout: Layout[]) => void
  onSelectCamera: (camera: Camera) => void
  onFocusCamera: (camera: Camera) => void
  readOnly?: boolean
}) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [containerSize, setContainerSize] = useState({ width: 0, height: 0 })
  const boundedLayout = useMemo(
    () => clampLayoutToGridBounds(layout, LIVE_GRID_MAX_ROWS, GRID_COLS),
    [layout],
  )
  const lastAcceptedLayoutRef = useRef<Layout[]>(boundedLayout)

  useEffect(() => {
    lastAcceptedLayoutRef.current = boundedLayout
  }, [boundedLayout])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const measure = () => setContainerSize({ width: el.offsetWidth, height: el.offsetHeight })
    measure()
    const resizeObserver = new ResizeObserver(measure)
    resizeObserver.observe(el)
    return () => resizeObserver.disconnect()
  }, [])

  const rowHeight = containerSize.height > 0
    ? calculateLiveGridRowHeight(containerSize.height, LIVE_GRID_MAX_ROWS, LIVE_GRID_MARGIN[1])
    : 1

  const handleBoundedLayoutChange = useCallback((nextLayout: Layout[]) => {
    if (readOnly) return
    const boundedNextLayout = getBoundedLayoutOrFallback(nextLayout, lastAcceptedLayoutRef.current, LIVE_GRID_MAX_ROWS, GRID_COLS)
    lastAcceptedLayoutRef.current = boundedNextLayout
    onLayoutChange(boundedNextLayout)
  }, [onLayoutChange, readOnly])

  if (readOnly) {
    const cameraById = new Map(cameras.map(camera => [camera.id, camera]))
    return (
      <div className="new-grid-stage new-viewer-static-grid">
        {boundedLayout.map(item => {
          const camera = cameraById.get(item.i)
          if (!camera) return null
          const style: CSSProperties = {
            position: 'absolute',
            left: `${(item.x / GRID_COLS) * 100}%`,
            top: `${(item.y / LIVE_GRID_MAX_ROWS) * 100}%`,
            width: `${(item.w / GRID_COLS) * 100}%`,
            height: `${(item.h / LIVE_GRID_MAX_ROWS) * 100}%`,
            padding: 2,
          }
          return (
            <div key={camera.id} className="new-grid-item" style={style}>
              <NewCameraTile
                camera={camera}
                hasMotion={motionCams.has(camera.id)}
                selected={camera.id === selectedCamId}
                onSelect={() => onSelectCamera(camera)}
                onFocus={() => onFocusCamera(camera)}
              />
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <div ref={containerRef} className="new-grid-stage">
      {containerSize.width > 0 && containerSize.height > 0 && (
        <GridLayout
          layout={boundedLayout}
          cols={GRID_COLS}
          rowHeight={rowHeight}
          width={containerSize.width}
          onLayoutChange={handleBoundedLayoutChange}
          draggableHandle=".cam-drag-handle"
          resizeHandles={['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']}
          resizeHandle={<ResizeHandle />}
          margin={LIVE_GRID_MARGIN}
          containerPadding={[0, 0]}
          maxRows={LIVE_GRID_MAX_ROWS}
          isBounded
          isDraggable={!readOnly}
          isResizable={!readOnly}
          autoSize={false}
          style={{ height: '100%' }}
        >
          {cameras.map(camera => (
            <div key={camera.id} className="new-grid-item">
              <NewCameraTile
                camera={camera}
                hasMotion={motionCams.has(camera.id)}
                selected={camera.id === selectedCamId}
                onSelect={() => onSelectCamera(camera)}
                onFocus={() => onFocusCamera(camera)}
              />
            </div>
          ))}
        </GridLayout>
      )}
    </div>
  )
}

function pctInDay(ts: number, dayStart: number, dayEnd: number): number {
  const value = ((ts - dayStart) / (dayEnd - dayStart)) * 100
  return Math.min(100, Math.max(0, value))
}

function TimelineBar({
  ranges,
  motionEvents,
  dayStart,
  dayEnd,
  cursorTs,
  aggregate,
  onSeek,
}: {
  ranges: TimelineRange[]
  motionEvents: TimelineMotion[]
  dayStart: number
  dayEnd: number
  cursorTs: number
  aggregate?: boolean
  onSeek?: (ts: number) => void
}) {
  const handleClick = (event: MouseEvent<HTMLDivElement>) => {
    if (!onSeek) return
    const rect = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - rect.left) / rect.width
    onSeek(dayStart + ratio * (dayEnd - dayStart))
  }

  return (
    <div className={`new-daybar ${aggregate ? 'new-aggregate' : ''}`} onClick={handleClick}>
      {ranges.map((range, index) => {
        const left = pctInDay(range.ts_start, dayStart, dayEnd)
        const right = pctInDay(range.ts_end, dayStart, dayEnd)
        return <span key={`${range.ts_start}-${index}`} className="new-chunk" style={{ left: `${left}%`, width: `${Math.max(right - left, 0.18)}%` }} />
      })}
      {motionEvents.map((event, index) => {
        const eventEnd = event.ts_end || event.ts_start + 5
        const left = pctInDay(event.ts_start, dayStart, dayEnd)
        const right = pctInDay(eventEnd, dayStart, dayEnd)
        return <span key={`motion-${event.ts_start}-${index}`} className="new-motion" style={{ left: `${left}%`, width: `${Math.max(right - left, 0.24)}%` }} />
      })}
      <span className="new-cursor" style={{ left: `${pctInDay(cursorTs, dayStart, dayEnd)}%` }} />
    </div>
  )
}

function NewTwoRowTimeline({
  cameras,
  timelineData,
  selectedCamId,
  date,
  collapsed = false,
  onToggleCollapsed,
  bodyId: providedBodyId,
  isLive = false,
  onSeek,
}: {
  cameras: Camera[]
  timelineData: Record<string, TimelineData>
  selectedCamId: string
  date: string
  collapsed?: boolean
  onToggleCollapsed?: () => void
  bodyId?: string
  isLive?: boolean
  onSeek?: (cameraId: string, ts: number) => void
}) {
  const [currentTime, setCurrentTime] = useState(new Date())

  useEffect(() => {
    if (!isLive) return
    const id = window.setInterval(() => setCurrentTime(new Date()), 1000)
    return () => window.clearInterval(id)
  }, [isLive])

  const dayStart = new Date(`${date}T00:00:00`).getTime() / 1000
  const dayEnd = dayStart + 86400
  const cursorTs = currentTime.getTime() / 1000
  const selectedCamera = cameras.find(camera => camera.id === selectedCamId) ?? cameras[0]
  const selectedData = selectedCamera ? timelineData[selectedCamera.id] : undefined
  const selectedRanges: TimelineRange[] = (selectedData?.segments ?? []).map(segment => ({
    ts_start: segment.ts_start,
    ts_end: segment.ts_end ?? cursorTs,
  }))
  const selectedMotion = selectedData?.motion_events ?? []
  const aggregateRanges = mergeTimelineRanges(cameras.flatMap(camera => timelineData[camera.id]?.segments ?? []), cursorTs)
  const aggregateMotion: TimelineMotion[] = cameras.flatMap(camera => timelineData[camera.id]?.motion_events ?? [])
  const bodyId = providedBodyId ?? `new-timeline-${date}-${selectedCamId || 'none'}`

  return (
    <footer className={`new-timeline ${collapsed ? 'new-collapsed' : ''}`} aria-label="선택 카메라와 전체 카메라 2줄 타임라인">
      <div className="new-timeline-top">
        <div className="new-clock">{format(currentTime, 'HH:mm:ss')}</div>
        <div className="new-mini">{date} · 1줄 선택 카메라 · 2줄 전체 집계</div>
        <div className="new-spacer" />
        {isLive && <div className="new-live-pill"><span className="new-pulse" />LIVE</div>}
        {onToggleCollapsed && (
          <button className="new-ghost" type="button" onClick={onToggleCollapsed} aria-controls={bodyId} aria-expanded={!collapsed}>
            {getTimelineToggleLabel(collapsed)}
          </button>
        )}
      </div>
      {!collapsed && (
        <div className="new-timeline-body" id={bodyId}>
          <div className="new-track">
            <div className="new-track-name"><strong>{selectedCamera?.name ?? '카메라 없음'}</strong>선택 카메라</div>
            <TimelineBar
              ranges={selectedRanges}
              motionEvents={selectedMotion}
              dayStart={dayStart}
              dayEnd={dayEnd}
              cursorTs={cursorTs}
              onSeek={selectedCamera && onSeek ? (ts) => onSeek(selectedCamera.id, ts) : undefined}
            />
          </div>
          <div className="new-track">
            <div className="new-track-name"><strong>전체 카메라</strong>녹화 있음</div>
            <TimelineBar
              ranges={aggregateRanges}
              motionEvents={aggregateMotion}
              dayStart={dayStart}
              dayEnd={dayEnd}
              cursorTs={cursorTs}
              aggregate
            />
          </div>
          <div className="new-ticks"><span />{['00', '04', '08', '12', '16', '20', '24'].map(tick => <span key={tick}>{tick}</span>)}</div>
        </div>
      )}
    </footer>
  )
}

function NewLivePage({
  cameras,
  page,
  onNavigate,
  viewerMode = false,
}: {
  cameras: Camera[]
  page: NewPage
  onNavigate: Navigate
  viewerMode?: boolean
}) {
  const today = format(new Date(), 'yyyy-MM-dd')
  const timelineData = useAllTimelines(cameras, today)
  const {
    layouts,
    currentId,
    gridLayout,
    isDirty,
    setGridLayout,
    loadLayout,
    saveLayout,
    saveAsLayout,
  } = useLayouts(cameras, { gridCols: GRID_COLS, gridRows: LIVE_GRID_MAX_ROWS })
  const [selectedCamId, setSelectedCamId] = useState('')
  const [focusedCamera, setFocusedCamera] = useState<Camera | null>(null)
  const [sidePanelHidden, setSidePanelHidden] = useState(false)
  const [layoutSaved, setLayoutSaved] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [liveTimelineCollapsed, setLiveTimelineCollapsed] = useState(() =>
    readNewLiveTimelineCollapsedPreference((key) => window.localStorage.getItem(key)),
  )
  const [motionNow, setMotionNow] = useState(() => Date.now() / 1000)
  const liveTimelineBodyId = 'new-live-timeline-body'

  useEffect(() => {
    const handler = () => setIsFullscreen(Boolean(document.fullscreenElement))
    document.addEventListener('fullscreenchange', handler)
    return () => document.removeEventListener('fullscreenchange', handler)
  }, [])

  useEffect(() => {
    const id = window.setInterval(() => setMotionNow(Date.now() / 1000), 1000)
    return () => window.clearInterval(id)
  }, [])

  const motionCams = useMemo(() => new Set(
    cameras
      .map(camera => camera.id)
      .filter(id => (timelineData[id]?.motion_events ?? []).some(event => motionNow - (event.ts_end ?? event.ts_start) < 30)),
  ), [cameras, motionNow, timelineData])

  const selectedCamera = cameras.find(camera => camera.id === selectedCamId) ?? cameras[0]
  const onlineCount = cameras.filter(camera => camera.online).length

  const flashSaved = () => {
    setLayoutSaved(true)
    window.setTimeout(() => setLayoutSaved(false), 1400)
  }

  const handleSave = async () => {
    if (currentId) {
      await saveLayout()
      flashSaved()
      return
    }
    const name = window.prompt('새 배치 이름', '신규 관제 배치')
    if (!name) return
    await saveAsLayout(name)
    flashSaved()
  }

  const handleSaveAs = async () => {
    const name = window.prompt('새 배치 이름', '신규 관제 배치')
    if (!name) return
    await saveAsLayout(name)
    flashSaved()
  }

  const handleSeek = (cameraId: string, ts: number) => {
    const segments = timelineData[cameraId]?.segments ?? []
    const segment = [...segments].reverse().find(item => item.ts_start <= ts && (item.ts_end ?? ts) >= ts)
    if (!segment) return
    const segmentDate = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Seoul' }).format(new Date(segment.ts_start * 1000))
    window.open(recordingUrl(cameraId, segmentDate, segment.filename), '_blank')
  }

  const toggleLiveTimelineCollapsed = () => {
    const next = !liveTimelineCollapsed
    window.localStorage.setItem(NEW_LIVE_TIMELINE_COLLAPSED_KEY, String(next))
    setLiveTimelineCollapsed(next)
  }

  const toggleFullscreen = () => {
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen()
    } else {
      document.exitFullscreen()
    }
  }

  return (
    <div className="new-app new-live-app">
      <NewHeader page={page} onNavigate={onNavigate} viewerMode={viewerMode}>
        <select className="new-select" value={currentId ?? ''} onChange={(event) => loadLayout(event.target.value, cameras)} aria-label="저장된 배치 선택">
          {layouts.length === 0 && <option value="">저장된 배치 없음</option>}
          {layouts.map(layout => <option key={layout.id} value={layout.id}>{layout.name}{layout.id === currentId && isDirty ? ' *' : ''}</option>)}
        </select>
        <button className="new-primary" type="button" onClick={handleSave}>{layoutSaved ? '저장됨' : '배치 저장'}</button>
        <button className="new-ghost" type="button" onClick={handleSaveAs}>새 이름 저장</button>
        <button className="new-ghost" type="button" onClick={() => setSidePanelHidden(value => !value)} aria-pressed={sidePanelHidden}>
          {sidePanelHidden ? '우측 패널 보기' : '우측 패널 숨기기'}
        </button>
        <button
          className={`${liveTimelineCollapsed ? 'new-primary' : 'new-ghost'} new-timeline-command`}
          type="button"
          onClick={toggleLiveTimelineCollapsed}
          aria-controls={liveTimelineBodyId}
          aria-expanded={!liveTimelineCollapsed}
        >
          {getTimelineToggleLabel(liveTimelineCollapsed)}
        </button>
        <div className="new-spacer" />
        <div className="new-live-pill"><span className="new-pulse" />LIVE</div>
        <button className="new-ghost" type="button" onClick={toggleFullscreen}>{isFullscreen ? '전체화면 종료' : '전체화면'}</button>
      </NewHeader>

      <section className={`new-live-workspace ${sidePanelHidden ? 'new-panel-hidden' : ''}`}>
        <main className="new-grid-wrap" aria-label="신규 라이브 카메라 그리드">
          {gridLayout.length > 0 ? (
            <NewCameraGrid
              cameras={cameras}
              layout={gridLayout}
              motionCams={motionCams}
              selectedCamId={selectedCamera?.id ?? ''}
              onLayoutChange={setGridLayout}
              onSelectCamera={(camera) => setSelectedCamId(camera.id)}
              onFocusCamera={(camera) => { setSelectedCamId(camera.id); setFocusedCamera(camera) }}
              readOnly={viewerMode}
            />
          ) : (
            <div className="new-empty">카메라와 배치 정보를 불러오는 중입니다.</div>
          )}
        </main>
        {!sidePanelHidden && (
          <aside className="new-side-panel" aria-label="운영 패널">
            <section className="new-panel-card">
              <div className="new-section-title">
                <span>저장된 배치 <em>{layouts.length} profiles</em></span>
                <button className="new-icon-button" type="button" onClick={() => setSidePanelHidden(true)} aria-label="우측 패널 숨기기">−</button>
              </div>
              <div className="new-layout-list">
                {layouts.map(layout => (
                  <button
                    key={layout.id}
                    type="button"
                    className={`new-layout-row ${layout.id === currentId ? 'new-active-row' : ''}`}
                    onClick={() => loadLayout(layout.id, cameras)}
                  >
                    <span>{layout.name}</span>
                    <em>{layout.id === currentId && isDirty ? '편집됨' : format(new Date(layout.updated_at * 1000), 'HH:mm')}</em>
                  </button>
                ))}
                {layouts.length === 0 && <div className="new-muted">저장된 배치가 없습니다. 현재 화면을 새 이름으로 저장할 수 있습니다.</div>}
              </div>
            </section>
            <section className="new-panel-card">
              <div className="new-section-title">카메라 상태 <em>{onlineCount} / {cameras.length} online</em></div>
              <div className="new-camera-list">
                {cameras.map(camera => (
                  <button key={camera.id} className={`new-camera-row ${camera.id === selectedCamera?.id ? 'new-active-row' : ''}`} type="button" onClick={() => setSelectedCamId(camera.id)}>
                    <span className={`new-state ${camera.online ? '' : 'new-danger'}`} />
                    <span>{camera.name}</span>
                    <em>{motionCams.has(camera.id) ? 'motion' : camera.online ? 'live' : 'offline'}</em>
                  </button>
                ))}
              </div>
            </section>
          </aside>
        )}
      </section>

      <NewTwoRowTimeline
        cameras={cameras}
        timelineData={timelineData}
        selectedCamId={selectedCamera?.id ?? ''}
        date={today}
        collapsed={liveTimelineCollapsed}
        onToggleCollapsed={toggleLiveTimelineCollapsed}
        bodyId={liveTimelineBodyId}
        isLive
        onSeek={viewerMode ? undefined : handleSeek}
      />

      {focusedCamera && (
        <div className="new-modal" role="dialog" aria-modal="true" aria-label={`${focusedCamera.name} 집중 보기`}>
          <div className="new-focus-view">
            <div className="new-focus-bar">
              <strong>{focusedCamera.name}</strong>
              <button className="new-ghost" type="button" onClick={() => setFocusedCamera(null)}>닫기</button>
            </div>
            <WebRTCPlayer camId={focusedCamera.has_sub ? `${focusedCamera.id}_sub` : focusedCamera.id} />
          </div>
        </div>
      )}
    </div>
  )
}

function NewRecordingsPage({ cameras, page, onNavigate }: { cameras: Camera[]; page: NewPage; onNavigate: Navigate }) {
  const [selectedDate, setSelectedDate] = useState(format(new Date(), 'yyyy-MM-dd'))
  const [selectedCamId, setSelectedCamId] = useState('')
  const [filter, setFilter] = useState<RecordingFilter>('all')
  const [segments, setSegments] = useState<RecordingSegment[]>([])
  const [selectedFilename, setSelectedFilename] = useState('')
  const [downloadReady, setDownloadReady] = useState(false)
  const timelineData = useAllTimelines(cameras, selectedDate)
  const activeCamId = selectedCamId || cameras[0]?.id || ''

  useEffect(() => {
    if (!activeCamId) return
    listRecordings(activeCamId, selectedDate)
      .then(data => {
        setSegments(data)
        setSelectedFilename('')
      })
      .catch(() => setSegments([]))
  }, [activeCamId, selectedDate])

  const selectedCamera = cameras.find(camera => camera.id === activeCamId) ?? cameras[0]
  const selectedMotionEvents = useMemo(
    () => (activeCamId ? timelineData[activeCamId]?.motion_events ?? [] : []),
    [activeCamId, timelineData],
  )
  const visibleSegments = useMemo(
    () => filterRecordingSegments(segments, selectedMotionEvents, filter),
    [segments, selectedMotionEvents, filter],
  )

  const activeFilename = visibleSegments.some(segment => segment.filename === selectedFilename)
    ? selectedFilename
    : visibleSegments[0]?.filename ?? ''
  const selectedSegment = visibleSegments.find(segment => segment.filename === activeFilename) ?? null
  const selectedUrl = selectedSegment && selectedCamera ? recordingUrl(selectedCamera.id, selectedDate, selectedSegment.filename) : ''
  const handleDownloadFeedback = () => {
    setDownloadReady(true)
    window.setTimeout(() => setDownloadReady(false), 1400)
  }

  const handleSeek = (cameraId: string, ts: number) => {
    if (cameraId !== selectedCamera?.id) setSelectedCamId(cameraId)
    const segment = [...segments].reverse().find(item => item.ts_start <= ts && (item.ts_end ?? ts) >= ts)
    if (segment) setSelectedFilename(segment.filename)
  }

  return (
    <div className="new-app new-recordings-app">
      <NewHeader page={page} onNavigate={onNavigate}>
        <input className="new-control" type="date" value={selectedDate} onChange={(event) => setSelectedDate(event.target.value)} aria-label="녹화 날짜" />
        <select className="new-select" value={activeCamId} onChange={(event) => setSelectedCamId(event.target.value)} aria-label="카메라 선택">
          {cameras.map(camera => <option key={camera.id} value={camera.id}>{camera.name}</option>)}
        </select>
        {selectedUrl ? (
          <a className="new-primary new-download-link" href={selectedUrl} download onClick={handleDownloadFeedback}>{downloadReady ? '다운로드 준비됨' : '선택 파일 다운로드'}</a>
        ) : (
          <span className="new-primary new-disabled">선택 파일 다운로드</span>
        )}
      </NewHeader>

      <main className="new-recordings-workspace">
        <aside className="new-recording-sidebar">
          <section className="new-panel-card">
            <label className="new-label">빠른 필터
              <select value={filter} onChange={(event) => setFilter(event.target.value as RecordingFilter)}>
                <option value="all">전체 녹화</option>
                <option value="motion">모션 발생 구간만</option>
                <option value="offline">오프라인 전후</option>
              </select>
            </label>
            <div className="new-filter-pills" aria-label="빠른 필터 버튼">
              {(['all', 'motion', 'offline'] as RecordingFilter[]).map(item => (
                <button key={item} className={filter === item ? 'new-active' : ''} type="button" onClick={() => setFilter(item)}>
                  {item === 'all' ? '전체' : item === 'motion' ? '모션' : '오프라인 전후'}
                </button>
              ))}
            </div>
          </section>
          <section className="new-segment-list" aria-label="녹화 세그먼트 목록">
            {visibleSegments.map(segment => {
              const duration = segment.ts_end ? segment.ts_end - segment.ts_start : 0
              return (
                <button
                  key={segment.filename}
                  type="button"
                  className={`new-segment ${segment.filename === activeFilename ? 'new-active-row' : ''}`}
                  onClick={() => setSelectedFilename(segment.filename)}
                >
                  <span className="new-seg-top"><strong>{formatClock(segment.ts_start)}{segment.ts_end ? ` - ${formatClock(segment.ts_end)}` : ''}</strong><em>{formatStorageSize(segment.file_size)}</em></span>
                  <span className="new-seg-meta"><span>{duration > 0 ? formatDuration(duration) : '진행 중'}</span><span>{segment.filename}</span></span>
                </button>
              )
            })}
            {visibleSegments.length === 0 && <div className="new-empty">조건에 맞는 녹화 세그먼트가 없습니다.</div>}
          </section>
        </aside>

        <section className="new-recording-content">
          <div className="new-player" aria-label="녹화 재생기">
            {selectedUrl ? (
              <video controls autoPlay src={selectedUrl} />
            ) : (
              <div className="new-player-placeholder">녹화 파일을 선택하세요.</div>
            )}
            <div className="new-player-meta">
              <strong>{selectedCamera?.name ?? '카메라 없음'}</strong>
              <span>{selectedSegment ? `${formatClock(selectedSegment.ts_start)} · ${selectedSegment.filename}` : '선택된 세그먼트 없음'}</span>
            </div>
          </div>
          <section className="new-timeline-card">
            <div className="new-card-head">
              <h1>{selectedCamera?.name ?? '카메라'} · {selectedDate}</h1>
              <span>선택 카메라 + 전체 중첩 타임라인</span>
            </div>
            <NewTwoRowTimeline
              cameras={cameras}
              timelineData={timelineData}
              selectedCamId={selectedCamera?.id ?? ''}
              date={selectedDate}
              onSeek={handleSeek}
            />
          </section>
        </section>
      </main>
    </div>
  )
}

function NewSettingsPage({ page, onNavigate }: { page: NewPage; onNavigate: Navigate }) {
  const [form, setForm] = useState<Settings>({
    retention_days: 30,
    segment_minutes: 10,
    motion_threshold: 0.02,
    max_storage_gb: 0,
    motion_enabled: true,
  })
  const [saved, setSaved] = useState(false)
  const [stats, setStats] = useState<StorageStats | null>(null)
  const [statsLoading, setStatsLoading] = useState(true)
  const [version, setVersion] = useState<SystemVersion | null>(null)
  const [viewerVersion, setViewerVersion] = useState<string | null>(null)
  const [cameraConfig, setCameraConfig] = useState<CameraAdminItem[]>([])
  const [cameraConfigLoading, setCameraConfigLoading] = useState(true)
  const [cameraToggling, setCameraToggling] = useState<string | null>(null)
  const [cameraRebooting, setCameraRebooting] = useState<string | null>(null)
  const [cameraArchiving, setCameraArchiving] = useState<string | null>(null)
  const [cameraApplying, setCameraApplying] = useState(false)
  const [cameraEditing, setCameraEditing] = useState(false)
  const [editingCameraId, setEditingCameraId] = useState<string | null>(null)
  const [cameraEditForm, setCameraEditForm] = useState<CameraEditForm | null>(null)
  const [cameraMessage, setCameraMessage] = useState<string | null>(null)
  const [updateLoading, setUpdateLoading] = useState(false)
  const [updateMessage, setUpdateMessage] = useState<string | null>(null)

  const loadVersion = useCallback(() => {
    getSystemVersion().then(setVersion).catch(console.error)
  }, [])

  const loadStorageStats = useCallback((retry = 1) => {
    setStatsLoading(true)
    getStorageStats()
      .then(setStats)
      .catch(error => {
        console.error(error)
        if (retry > 0) window.setTimeout(() => loadStorageStats(retry - 1), 1200)
      })
      .finally(() => setStatsLoading(false))
  }, [])

  const loadCameraConfig = useCallback((retry = 1) => {
    setCameraConfigLoading(true)
    getCameraAdmin()
      .then(data => {
        setCameraConfig(data)
        setCameraMessage(null)
      })
      .catch(error => {
        console.error(error)
        if (retry > 0) {
          window.setTimeout(() => loadCameraConfig(retry - 1), 1200)
        } else {
          setCameraMessage('카메라 설정을 불러오지 못했습니다. 새로고침을 눌러 다시 시도하세요.')
        }
      })
      .finally(() => setCameraConfigLoading(false))
  }, [])

  useEffect(() => {
    getSettings().then(setForm).catch(console.error)
    loadStorageStats()
    getViewerVersion().then(data => setViewerVersion(data.version)).catch(() => {})
    loadVersion()
    loadCameraConfig()
  }, [loadVersion, loadCameraConfig, loadStorageStats])

  const saveSettings = async () => {
    await updateSettings(form)
    setSaved(true)
    window.setTimeout(() => setSaved(false), 1600)
  }

  const handleToggleCamera = async (camera: CameraAdminItem) => {
    const nextEnabled = !camera.enabled
    setCameraToggling(camera.id)
    setCameraMessage(null)
    try {
      const updated = await setCameraAdminEnabled(camera.id, nextEnabled)
      setCameraConfig(previous => previous.map(item => item.id === updated.id ? updated : item))
      setCameraMessage(`${camera.id} ${nextEnabled ? '활성화' : '비활성화'} 완료. 설정 적용을 누르면 go2rtc.yaml에 반영됩니다.`)
    } catch (error) {
      console.error(error)
      setCameraMessage(`${camera.id} ${nextEnabled ? '활성화' : '비활성화'} 실패.`)
    } finally {
      setCameraToggling(null)
    }
  }

  const handleRebootCamera = async (camera: CameraAdminItem) => {
    if (!window.confirm(`${camera.display_name} 카메라를 ONVIF로 재부팅할까요?`)) return
    setCameraRebooting(camera.id)
    setCameraMessage(null)
    try {
      await rebootCamera(camera.id)
      setCameraMessage(`${camera.id} 재부팅 요청 전송 완료. 카메라가 1~2분 정도 오프라인일 수 있습니다.`)
    } catch (error) {
      console.error(error)
      setCameraMessage(`${camera.id} 재부팅 요청 실패.`)
    } finally {
      setCameraRebooting(null)
    }
  }

  const handleArchiveCamera = async (camera: CameraAdminItem) => {
    if (!window.confirm(`${camera.display_name} 카메라를 아카이브할까요? 과거 녹화/모션 데이터는 보존됩니다.`)) return
    setCameraArchiving(camera.id)
    setCameraMessage(null)
    try {
      const archived = await archiveCameraAdmin(camera.id)
      setCameraConfig(previous => previous.filter(item => item.id !== archived.id))
      setCameraMessage(`${camera.id} 아카이브 완료. 설정 적용을 누르면 go2rtc.yaml에서 제외됩니다.`)
    } catch (error) {
      console.error(error)
      setCameraMessage(`${camera.id} 아카이브 실패.`)
    } finally {
      setCameraArchiving(null)
    }
  }

  const handleApplyCameraConfig = async () => {
    setCameraApplying(true)
    setCameraMessage(null)
    try {
      const result = await applyCameraAdminConfig()
      setCameraMessage(result.changed ? `카메라 설정 적용 완료: go2rtc 재시작, 녹화 조정, Viewer reload ${result.viewer_reload_commands}건 예약.` : '적용할 변경 사항이 없습니다.')
      loadCameraConfig()
    } catch (error) {
      console.error(error)
      setCameraMessage('카메라 설정 적용 실패.')
    } finally {
      setCameraApplying(false)
    }
  }

  const handleCreateCamera = async () => {
    const id = window.prompt('새 카메라 ID를 입력하세요. 생성 후 변경하지 않는 내부 식별자입니다.')?.trim()
    if (!id) return
    const displayName = window.prompt('표시명을 입력하세요.', id)?.trim()
    if (!displayName) return
    const mainStreamUrl = window.prompt('메인 RTSP URL을 입력하세요. 이 값은 화면에 다시 노출되지 않습니다.')?.trim()
    if (!mainStreamUrl) return
    const subStreamUrl = window.prompt('보조 RTSP URL을 입력하세요. 없으면 비워두세요.')?.trim() || null
    const location = window.prompt('위치/그룹을 입력하세요. 없으면 비워두세요.')?.trim() || null
    setCameraEditing(true)
    setCameraMessage(null)
    try {
      const created = await createCameraAdmin({
        id,
        display_name: displayName,
        location,
        enabled: true,
        main_stream_url: mainStreamUrl,
        sub_stream_url: subStreamUrl,
      })
      setCameraConfig(previous => [...previous, created].sort((a, b) => a.sort_order - b.sort_order || a.id.localeCompare(b.id)))
      setCameraMessage(`${created.id} 추가 완료. 설정 적용을 누르면 go2rtc.yaml에 반영됩니다.`)
    } catch (error) {
      console.error(error)
      setCameraMessage(`${id} 추가 실패.`)
    } finally {
      setCameraEditing(false)
    }
  }

  const startEditCamera = (camera: CameraAdminItem) => {
    setEditingCameraId(camera.id)
    setCameraEditForm({
      display_name: camera.display_name,
      location: camera.location ?? '',
      notes: camera.notes ?? '',
      main_stream_url: '',
      sub_stream_url: '',
      onvif_host: '',
      onvif_port: '',
      onvif_username: '',
      onvif_password: '',
    })
    setCameraMessage(null)
  }

  const cancelEditCamera = () => {
    setEditingCameraId(null)
    setCameraEditForm(null)
  }

  const updateEditField = (key: keyof CameraEditForm, value: string) => {
    setCameraEditForm(previous => previous ? { ...previous, [key]: value } : previous)
  }

  const optionalSecretField = (value: string) => {
    const trimmed = value.trim()
    if (!trimmed) return undefined
    if (trimmed === '__CLEAR__') return null
    return trimmed
  }

  const saveEditCamera = async (camera: CameraAdminItem) => {
    if (!cameraEditForm) return
    const displayName = cameraEditForm.display_name.trim()
    if (!displayName) {
      setCameraMessage('표시명은 비워둘 수 없습니다.')
      return
    }
    const onvifPortField = optionalSecretField(cameraEditForm.onvif_port)
    const onvifPort = onvifPortField === undefined || onvifPortField === null ? onvifPortField : Number(onvifPortField)
    if (typeof onvifPort === 'number' && (!Number.isInteger(onvifPort) || onvifPort <= 0 || onvifPort > 65535)) {
      setCameraMessage('ONVIF 포트는 1~65535 사이 숫자여야 합니다.')
      return
    }
    const payload: CameraAdminUpdateRequest = {
      display_name: displayName,
      location: cameraEditForm.location.trim() || null,
      notes: cameraEditForm.notes.trim() || null,
    }
    const mainStreamUrl = optionalSecretField(cameraEditForm.main_stream_url)
    const subStreamUrl = optionalSecretField(cameraEditForm.sub_stream_url)
    const onvifHost = optionalSecretField(cameraEditForm.onvif_host)
    const onvifUsername = optionalSecretField(cameraEditForm.onvif_username)
    const onvifPassword = optionalSecretField(cameraEditForm.onvif_password)
    if (mainStreamUrl !== undefined) payload.main_stream_url = mainStreamUrl
    if (subStreamUrl !== undefined) payload.sub_stream_url = subStreamUrl
    if (onvifHost !== undefined) payload.onvif_host = onvifHost
    if (onvifPort !== undefined) payload.onvif_port = onvifPort
    if (onvifUsername !== undefined) payload.onvif_username = onvifUsername
    if (onvifPassword !== undefined) payload.onvif_password = onvifPassword

    setCameraEditing(true)
    setCameraMessage(null)
    try {
      const updated = await updateCameraAdmin(camera.id, payload)
      setCameraConfig(previous => previous.map(item => item.id === updated.id ? updated : item))
      setCameraMessage(`${camera.id} 수정 완료. 연결정보를 변경했다면 설정 적용을 눌러 go2rtc.yaml에 반영하세요.`)
      cancelEditCamera()
    } catch (error) {
      console.error(error)
      setCameraMessage(`${camera.id} 수정 실패.`)
    } finally {
      setCameraEditing(false)
    }
  }

  const runUpdate = async () => {
    setUpdateLoading(true)
    setUpdateMessage(null)
    const startingVersion = version?.current_version
    try {
      const result = await triggerUpdate()
      if (result.status === 'already_running') {
        setUpdateMessage('이미 업데이트 진행 중입니다.')
        setUpdateLoading(false)
        return
      }
      setUpdateMessage('업데이트 중... 완료되면 자동으로 새로고침됩니다.')
      const deadline = Date.now() + 3 * 60 * 1000
      const poll = async () => {
        if (Date.now() > deadline) {
          setUpdateMessage('업데이트 시간 초과. 페이지를 수동으로 새로고침하세요.')
          setUpdateLoading(false)
          return
        }
        try {
          const nextVersion = await getSystemVersion()
          if (nextVersion.current_version !== startingVersion) {
            window.location.reload()
            return
          }
        } catch {
          // 백엔드 재시작 중일 수 있어 계속 폴링합니다.
        }
        window.setTimeout(poll, 3000)
      }
      window.setTimeout(poll, 5000)
    } catch {
      setUpdateMessage('업데이트 요청 실패.')
      setUpdateLoading(false)
    }
  }

  const setNumber = (key: 'retention_days' | 'segment_minutes' | 'motion_threshold' | 'max_storage_gb', value: string) => {
    setForm(previous => ({ ...previous, [key]: Number(value) }))
  }

  const diskPct = stats && stats.disk_total_gb > 0 ? (stats.disk_used_gb / stats.disk_total_gb) * 100 : 0
  const estimatedDays = stats && stats.hourly_gb_total > 0 && form.max_storage_gb > 0
    ? Math.floor(form.max_storage_gb / (stats.hourly_gb_total * 24))
    : form.retention_days

  return (
    <div className="new-app new-settings-app">
      <NewHeader page={page} onNavigate={onNavigate}>
        <button className="new-primary new-settings-save" type="button" onClick={saveSettings}>{saved ? '저장됨' : '설정 저장'}</button>
      </NewHeader>

      <main className="new-settings-content">
        <section className="new-settings-main">
          <article className="new-card new-storage-card">
            <h1>저장소 현황</h1>
            {statsLoading ? (
              <div className="new-muted">저장소 통계를 불러오는 중입니다.</div>
            ) : stats ? (
              <>
                <div className="new-storage-bar"><span style={{ width: `${Math.min(100, diskPct)}%` }} /></div>
                <div className="new-stats-grid">
                  <div className="new-stat"><strong>{stats.recordings_gb.toFixed(1)}GB</strong><span>녹화 데이터</span></div>
                  <div className="new-stat"><strong>{diskPct.toFixed(0)}%</strong><span>디스크 사용량</span></div>
                  <div className="new-stat"><strong>{estimatedDays}일</strong><span>예상 보관 가능</span></div>
                </div>
              </>
            ) : (
              <div className="new-muted new-warn">저장소 통계를 불러올 수 없습니다.</div>
            )}
          </article>

          <article className="new-card">
            <h2>녹화 정책</h2>
            <div className="new-form-grid">
              <label className="new-label">보존 기간
                <input type="number" value={form.retention_days} onChange={(event) => setNumber('retention_days', event.target.value)} />
              </label>
              <label className="new-label">세그먼트 길이
                <input type="number" value={form.segment_minutes} onChange={(event) => setNumber('segment_minutes', event.target.value)} />
              </label>
              <label className="new-label">모션 감도 임계값
                <input type="number" step="0.01" value={form.motion_threshold} onChange={(event) => setNumber('motion_threshold', event.target.value)} />
              </label>
              <label className="new-label">자동 삭제 한도 GB
                <input type="number" value={form.max_storage_gb} onChange={(event) => setNumber('max_storage_gb', event.target.value)} />
              </label>
            </div>
          </article>

          <article className="new-card">
            <h2>카메라별 저장량</h2>
            <div className="new-table-wrap">
              <table>
                <thead><tr><th>카메라</th><th>총 용량</th><th>시간당</th><th>보관일</th><th>가장 오래된 날짜</th></tr></thead>
                <tbody>
                  {(stats?.cameras ?? []).sort((a, b) => b.total_gb - a.total_gb).map(camera => (
                    <tr key={camera.camera_id}>
                      <td>{camera.camera_id}</td>
                      <td>{camera.total_gb.toFixed(1)} GB</td>
                      <td>{camera.hourly_gb >= 1 ? `${camera.hourly_gb.toFixed(2)} GB/h` : `${(camera.hourly_gb * 1024).toFixed(0)} MB/h`}</td>
                      <td>{camera.days_recorded}일</td>
                      <td>{camera.oldest_date ?? '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </article>
        </section>

        <aside className="new-settings-side">
          <article className="new-card">
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12 }}>
              <h2>카메라 관리</h2>
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="new-ghost" type="button" disabled={cameraConfigLoading || cameraToggling !== null || cameraRebooting !== null || cameraArchiving !== null || cameraApplying || cameraEditing} onClick={() => loadCameraConfig()}>새로고침</button>
                <button className="new-ghost" type="button" disabled={cameraConfigLoading || cameraApplying || cameraEditing} onClick={handleCreateCamera}>{cameraEditing ? '처리 중...' : '카메라 추가'}</button>
                <button className="new-primary" type="button" disabled={cameraConfigLoading || cameraApplying || cameraEditing} onClick={handleApplyCameraConfig}>{cameraApplying ? '적용 중...' : '설정 적용'}</button>
              </div>
            </div>
            <p className="new-muted">DB의 카메라 마스터를 관리합니다. 활성/비활성·아카이브 후 설정 적용을 누르면 go2rtc.yaml에 반영됩니다. 재부팅은 ONVIF SystemReboot 요청입니다.</p>
            {cameraConfigLoading ? (
              <div className="new-muted">카메라 설정을 불러오는 중입니다.</div>
            ) : cameraConfig.length === 0 ? (
              <div className="new-muted">카메라 설정이 없습니다.</div>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {cameraConfig.map(camera => {
                  const busy = cameraToggling === camera.id
                  const rebooting = cameraRebooting === camera.id
                  const archiving = cameraArchiving === camera.id
                  const controlsDisabled = cameraToggling !== null || cameraRebooting !== null || cameraArchiving !== null || cameraApplying || cameraEditing
                  return (
                    <div key={camera.id} style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: 10, alignItems: 'center', borderTop: '1px solid rgba(255,255,255,0.08)', paddingTop: 10 }}>
                      <div>
                        <strong>{camera.display_name}</strong>
                        <div className="new-muted">{camera.id} · {camera.enabled ? (camera.online ? '온라인' : '오프라인') : '비활성화'} · 보조 스트림 {camera.has_sub ? '있음' : '없음'} · ONVIF {camera.onvif_configured ? '설정됨' : '미설정'}</div>
                      </div>
                      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
                        <button className="new-ghost" type="button" disabled={controlsDisabled || !camera.onvif_configured} onClick={() => handleRebootCamera(camera)}>{rebooting ? '요청 중...' : '재부팅'}</button>
                        <button className="new-ghost" type="button" disabled={controlsDisabled && editingCameraId !== camera.id} onClick={() => startEditCamera(camera)}>수정</button>
                        <button className={camera.enabled ? 'new-ghost' : 'new-primary'} type="button" disabled={controlsDisabled} onClick={() => handleToggleCamera(camera)}>{busy ? '처리 중...' : camera.enabled ? '비활성화' : '활성화'}</button>
                        <button className="new-ghost" type="button" disabled={controlsDisabled} onClick={() => handleArchiveCamera(camera)}>{archiving ? '보관 중...' : '아카이브'}</button>
                      </div>
                      {editingCameraId === camera.id && cameraEditForm && (
                        <div style={{ gridColumn: '1 / -1', border: '1px solid rgba(0,191,174,0.35)', borderRadius: 16, padding: 12, background: 'rgba(0,191,174,0.06)' }}>
                          <div className="new-muted" style={{ marginBottom: 10 }}>연결정보는 보안상 기존 값을 표시하지 않습니다. 빈칸은 기존 값 유지, 지우려면 __CLEAR__ 입력.</div>
                          <div className="new-form-grid">
                            <label className="new-label">표시명
                              <input value={cameraEditForm.display_name} onChange={(event) => updateEditField('display_name', event.target.value)} />
                            </label>
                            <label className="new-label">위치/그룹
                              <input value={cameraEditForm.location} onChange={(event) => updateEditField('location', event.target.value)} />
                            </label>
                            <label className="new-label">메인 RTSP URL
                              <input value={cameraEditForm.main_stream_url} placeholder={camera.main_stream_configured ? '기존 값 유지' : '미설정'} onChange={(event) => updateEditField('main_stream_url', event.target.value)} />
                            </label>
                            <label className="new-label">보조 RTSP URL
                              <input value={cameraEditForm.sub_stream_url} placeholder={camera.sub_stream_configured ? '기존 값 유지' : '미설정'} onChange={(event) => updateEditField('sub_stream_url', event.target.value)} />
                            </label>
                            <label className="new-label">ONVIF 호스트/IP
                              <input value={cameraEditForm.onvif_host} placeholder={camera.onvif_configured ? '기존 값 유지' : '미설정'} onChange={(event) => updateEditField('onvif_host', event.target.value)} />
                            </label>
                            <label className="new-label">ONVIF 포트
                              <input value={cameraEditForm.onvif_port} placeholder={camera.onvif_configured ? '기존 값 유지' : '예: 80'} onChange={(event) => updateEditField('onvif_port', event.target.value)} />
                            </label>
                            <label className="new-label">ONVIF 사용자명
                              <input value={cameraEditForm.onvif_username} placeholder={camera.onvif_configured ? '기존 값 유지' : '미설정'} onChange={(event) => updateEditField('onvif_username', event.target.value)} />
                            </label>
                            <label className="new-label">ONVIF 비밀번호
                              <input type="password" value={cameraEditForm.onvif_password} placeholder={camera.onvif_configured ? '기존 값 유지' : '미설정'} onChange={(event) => updateEditField('onvif_password', event.target.value)} />
                            </label>
                            <label className="new-label" style={{ gridColumn: '1 / -1' }}>운영 메모
                              <input value={cameraEditForm.notes} onChange={(event) => updateEditField('notes', event.target.value)} />
                            </label>
                          </div>
                          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 12 }}>
                            <button className="new-ghost" type="button" disabled={cameraEditing} onClick={cancelEditCamera}>취소</button>
                            <button className="new-primary" type="button" disabled={cameraEditing} onClick={() => saveEditCamera(camera)}>{cameraEditing ? '저장 중...' : '수정 저장'}</button>
                          </div>
                        </div>
                      )}
                    </div>
                  )
                })}
              </div>
            )}
            {cameraMessage && <p className={`new-muted ${cameraMessage.includes('실패') ? 'new-warn' : 'new-ok'}`}>{cameraMessage}</p>}
          </article>
          <article className="new-card">
            <h2>감지 설정</h2>
            <label className="new-toggle">
              <span><strong>모션 감지 활성화</strong><em>타임라인의 주황 이벤트와 녹화 필터에 사용됩니다.</em></span>
              <input type="checkbox" checked={form.motion_enabled} onChange={(event) => setForm(previous => ({ ...previous, motion_enabled: event.target.checked }))} />
            </label>
          </article>
          <article className="new-card">
            <h2>시스템 업데이트</h2>
            <div className="new-version-row"><span>현재 버전</span><strong>{version?.current_version ?? '확인 중'}</strong></div>
            <div className="new-version-row"><span>최신 버전</span><strong className={version?.update_available ? 'new-warn' : 'new-ok'}>{version?.latest_version ?? '확인 불가'}</strong></div>
            <button className="new-ghost" type="button" disabled={updateLoading || !version?.update_available} onClick={runUpdate}>
              {updateLoading ? '업데이트 중...' : version?.update_available ? '지금 업데이트' : '최신 상태'}
            </button>
            {updateMessage && <p className="new-muted">{updateMessage}</p>}
          </article>
          <article className="new-card">
            <h2>Viewer App</h2>
            <div className="new-version-row"><span>Windows 빌드</span><strong>{viewerVersion ?? '배포 없음'}</strong></div>
            <a className={`new-primary ${viewerVersion ? '' : 'new-disabled'}`} href="/api/settings/viewer-app" download="CamViewer.exe">CamViewer.exe 다운로드</a>
          </article>
        </aside>
      </main>
    </div>
  )
}

export function NewCamStation() {
  const viewerMode = isNewViewerMode(window.location.search)
  const [page, setPage] = useState<NewPage>(() => initialPageFromPath(window.location.pathname, viewerMode))
  const cameras = useCameras()
  useViewerHeartbeat(viewerMode, cameras)

  useEffect(() => {
    if (viewerMode && window.location.pathname !== '/new') {
      window.history.replaceState(null, '', '/new?viewer=1')
    }
  }, [viewerMode])

  useEffect(() => {
    const onPopState = () => {
      const nextViewerMode = isNewViewerMode(window.location.search)
      const nextPage = initialPageFromPath(window.location.pathname, nextViewerMode)
      if (nextViewerMode && window.location.pathname !== '/new') {
        window.history.replaceState(null, '', '/new?viewer=1')
      }
      setPage(nextPage)
    }
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [])

  if (!viewerMode && page === 'recordings') return <NewRecordingsPage cameras={cameras} page={page} onNavigate={setPage} />
  if (!viewerMode && page === 'settings') return <NewSettingsPage page={page} onNavigate={setPage} />
  return <NewLivePage cameras={cameras} page="live" onNavigate={setPage} viewerMode={viewerMode} />
}
