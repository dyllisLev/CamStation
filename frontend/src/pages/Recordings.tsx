import { useState, useEffect } from 'react';
import { useCameras } from '../hooks/useCameras';
import { format } from 'date-fns';
import { listRecordings } from '../api/client';

export function RecordingsPage() {
  const cameras = useCameras();
  const [selectedCam, setSelectedCam] = useState('');
  const [selectedDate, setSelectedDate] = useState(format(new Date(), 'yyyy-MM-dd'));
  const [files, setFiles] = useState<string[]>([]);
  const [playingFile, setPlayingFile] = useState<string | null>(null);

  useEffect(() => {
    if (cameras.length > 0 && !selectedCam) setSelectedCam(cameras[0].id);
  }, [cameras]);

  useEffect(() => {
    if (!selectedCam || !selectedDate) return;
    setPlayingFile(null);
    listRecordings(selectedCam, selectedDate)
      .then(setFiles)
      .catch(() => setFiles([]));
  }, [selectedCam, selectedDate]);

  const downloadUrl = (filename: string) =>
    `/api/recordings/${encodeURIComponent(selectedCam)}/${selectedDate}/${filename}`;

  const formatFilename = (f: string) => {
    const noExt = f.replace(/\.[^.]+$/, '');
    const match = noExt.match(/(\d{4}-\d{2}-\d{2})[_-]?(\d{2}[-:]?\d{2}[-:]?\d{2})?/);
    if (match) {
      const time = match[2]?.replace(/-/g, ':') ?? '';
      return time ? `${match[1]} ${time}` : match[1];
    }
    const hm = noExt.match(/^(\d{2})-(\d{2})$/);
    if (hm) return `${selectedDate} ${hm[1]}:${hm[2]}`;
    return f;
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
        <span style={{ color: '#666', fontSize: 12 }}>{files.length}개 파일</span>
      </div>

      {files.length === 0 && (
        <p style={{ color: '#555', fontSize: 13 }}>녹화 파일이 없습니다.</p>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
        {files.map(f => (
          <div key={f}>
            <div
              style={{
                display: 'flex', alignItems: 'center', gap: 12,
                background: playingFile === f ? '#333' : '#222',
                padding: '10px 14px', borderRadius: playingFile === f ? '4px 4px 0 0' : 4,
                borderLeft: playingFile === f ? '2px solid #64b5f6' : '2px solid transparent',
                cursor: 'pointer',
              }}
              onClick={() => setPlayingFile(playingFile === f ? null : f)}
            >
              <span style={{ fontSize: 18, opacity: 0.5 }}>▶</span>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: 13, color: '#ddd', fontVariantNumeric: 'tabular-nums' }}>
                  {formatFilename(f)}
                </div>
                <div style={{ fontSize: 11, color: '#666', marginTop: 2 }}>{f}</div>
              </div>
              <a
                href={downloadUrl(f)}
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

            {playingFile === f && (
              <div style={{ background: '#1a1a1a', borderRadius: '0 0 4px 4px', padding: 12 }}>
                <video
                  controls
                  autoPlay
                  src={downloadUrl(f)}
                  style={{ width: '100%', maxHeight: 400, background: '#000', borderRadius: 3, display: 'block' }}
                />
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
