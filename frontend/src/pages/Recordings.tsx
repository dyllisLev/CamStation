import { useState, useEffect } from 'react';
import { useCameras } from '../hooks/useCameras';
import { format } from 'date-fns';
import { listRecordings } from '../api/client';
import type { RecordingSegment } from '../types';

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}분 ${s.toString().padStart(2, '0')}초`;
}

function formatSize(bytes: number | null): string {
  if (!bytes) return '';
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`;
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(0)} MB`;
  return `${(bytes / 1024).toFixed(0)} KB`;
}

export function RecordingsPage() {
  const cameras = useCameras();
  const [selectedCam, setSelectedCam] = useState('');
  const [selectedDate, setSelectedDate] = useState(format(new Date(), 'yyyy-MM-dd'));
  const [segments, setSegments] = useState<RecordingSegment[]>([]);
  const [playingFile, setPlayingFile] = useState<string | null>(null);

  useEffect(() => {
    if (cameras.length > 0 && !selectedCam) setSelectedCam(cameras[0].id);
  }, [cameras]);

  useEffect(() => {
    if (!selectedCam || !selectedDate) return;
    setPlayingFile(null);
    listRecordings(selectedCam, selectedDate)
      .then(setSegments)
      .catch(() => setSegments([]));
  }, [selectedCam, selectedDate]);

  const downloadUrl = (filename: string) =>
    `/api/recordings/${encodeURIComponent(selectedCam)}/${selectedDate}/${filename}`;

  const formatTime = (ts: number) => {
    const d = new Date(ts * 1000);
    return `${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
  };

  return (
    <div style={{ padding: 24, color: '#eee', maxWidth: 900 }}>
      <h2 style={{ marginBottom: 16, fontSize: 16, fontWeight: 600 }}>녹화 목록</h2>

      <div style={{ display: 'flex', gap: 10, marginBottom: 20, alignItems: 'center' }}>
        <select
          value={selectedCam}
          onChange={e => setSelectedCam(e.target.value)}
          style={{
            background: '#2a2a2a', border: '1px solid #444', color: '#eee',
            padding: '6px 10px', borderRadius: 4, fontSize: 13,
          }}
        >
          <option value="">카메라 선택</option>
          {cameras.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
        <input
          type="date"
          value={selectedDate}
          onChange={e => setSelectedDate(e.target.value)}
          style={{
            background: '#2a2a2a', border: '1px solid #444', color: '#eee',
            padding: '6px 10px', borderRadius: 4, fontSize: 13,
          }}
        />
        <span style={{ color: '#666', fontSize: 12 }}>{segments.length}개 파일</span>
      </div>

      {segments.length === 0 && (
        <p style={{ color: '#555', fontSize: 13 }}>녹화 파일이 없습니다.</p>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        {segments.map(seg => {
          const duration = seg.ts_end ? seg.ts_end - seg.ts_start : null;
          return (
            <div key={seg.filename}>
              <div
                style={{
                  display: 'flex', alignItems: 'center', gap: 12,
                  background: playingFile === seg.filename ? '#333' : '#222',
                  padding: '10px 14px', borderRadius: playingFile === seg.filename ? '4px 4px 0 0' : 4,
                  borderLeft: playingFile === seg.filename ? '2px solid #64b5f6' : '2px solid transparent',
                  cursor: 'pointer',
                }}
                onClick={() => setPlayingFile(playingFile === seg.filename ? null : seg.filename)}
              >
                <span style={{ fontSize: 18, opacity: 0.5 }}>▶</span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontSize: 13, color: '#ddd', fontVariantNumeric: 'tabular-nums' }}>
                    {formatTime(seg.ts_start)}
                    {seg.ts_end && ` – ${formatTime(seg.ts_end)}`}
                  </div>
                  <div style={{ fontSize: 11, color: '#666', marginTop: 2, display: 'flex', gap: 10 }}>
                    <span>{seg.filename}</span>
                    {duration && <span>{formatDuration(duration)}</span>}
                    {seg.file_size && <span>{formatSize(seg.file_size)}</span>}
                  </div>
                </div>
                <a
                  href={downloadUrl(seg.filename)}
                  download
                  onClick={e => e.stopPropagation()}
                  style={{
                    color: '#64b5f6', fontSize: 12, textDecoration: 'none',
                    padding: '4px 10px', border: '1px solid #335577', borderRadius: 3,
                    whiteSpace: 'nowrap',
                  }}
                >
                  ⬇ 다운로드
                </a>
              </div>

              {playingFile === seg.filename && (
                <div style={{ background: '#1a1a1a', borderRadius: '0 0 4px 4px', padding: 12 }}>
                  <video
                    controls
                    autoPlay
                    src={downloadUrl(seg.filename)}
                    style={{ width: '100%', maxHeight: 400, background: '#000', borderRadius: 3, display: 'block' }}
                  />
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
