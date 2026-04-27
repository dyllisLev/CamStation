import { useState, useEffect } from 'react';
import { useCameras } from '../hooks/useCameras';
import { format } from 'date-fns';
import axios from 'axios';

export function RecordingsPage() {
  const cameras = useCameras();
  const [selectedCam, setSelectedCam] = useState('');
  const [selectedDate, setSelectedDate] = useState(format(new Date(), 'yyyy-MM-dd'));
  const [files, setFiles] = useState<string[]>([]);

  useEffect(() => {
    if (!selectedCam || !selectedDate) return;
    axios.get(`/api/recordings/${encodeURIComponent(selectedCam)}/${selectedDate}`)
      .then(r => setFiles(r.data))
      .catch(() => setFiles([]));
  }, [selectedCam, selectedDate]);

  const downloadUrl = (filename: string) =>
    `/api/recordings/${encodeURIComponent(selectedCam)}/${selectedDate}/${filename}`;

  return (
    <div style={{ padding: 24, color: '#eee' }}>
      <h2 style={{ marginBottom: 16, fontSize: 16 }}>녹화 목록</h2>
      <div style={{ display: 'flex', gap: 12, marginBottom: 16, alignItems: 'center' }}>
        <select
          value={selectedCam}
          onChange={e => setSelectedCam(e.target.value)}
          style={{ background: '#2a2a2a', border: '1px solid #444', color: '#eee', padding: '4px 8px', borderRadius: 4 }}
        >
          <option value="">카메라 선택</option>
          {cameras.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
        </select>
        <input
          type="date"
          value={selectedDate}
          onChange={e => setSelectedDate(e.target.value)}
          style={{ background: '#2a2a2a', border: '1px solid #444', color: '#eee', padding: '4px 8px', borderRadius: 4 }}
        />
      </div>
      {files.length === 0 && <p style={{ color: '#555' }}>녹화 파일 없음</p>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {files.map(f => (
          <div key={f} style={{ display: 'flex', alignItems: 'center', gap: 12, background: '#2a2a2a', padding: '6px 12px', borderRadius: 4 }}>
            <span style={{ flex: 1, fontSize: 13 }}>{f}</span>
            <a href={downloadUrl(f)} download style={{ color: '#64b5f6', fontSize: 12, textDecoration: 'none' }}>⬇ 다운로드</a>
            <video controls src={downloadUrl(f)} style={{ width: 200, height: 112, background: '#111' }} />
          </div>
        ))}
      </div>
    </div>
  );
}
