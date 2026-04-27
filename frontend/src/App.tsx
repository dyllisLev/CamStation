import { useState } from 'react';
import { LiveView } from './pages/LiveView';
import { RecordingsPage } from './pages/Recordings';
import { SettingsPage } from './pages/Settings';

type Page = 'live' | 'recordings' | 'settings';

export default function App() {
  const [page, setPage] = useState<Page>('live');

  const nav = (
    <div style={{
      background: '#242424', borderBottom: '1px solid #333',
      padding: '6px 12px', display: 'flex', alignItems: 'center', gap: 10,
    }}>
      <span style={{ fontSize: 13, fontWeight: 'bold', color: '#64b5f6' }}>📷 CamStation</span>
      {(['live', 'recordings', 'settings'] as Page[]).map(p => (
        <button key={p} onClick={() => setPage(p)} style={{
          background: page === p ? '#1a3a5c' : 'none',
          border: 'none', borderRadius: 4, color: page === p ? '#64b5f6' : '#777',
          fontSize: 11, padding: '3px 8px', cursor: 'pointer',
        }}>
          {p === 'live' ? '라이브' : p === 'recordings' ? '녹화목록' : '설정'}
        </button>
      ))}
    </div>
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: '#1a1a1a' }}>
      {page !== 'live' && nav}
      {page === 'live' && <LiveView />}
      {page === 'recordings' && <RecordingsPage />}
      {page === 'settings' && <SettingsPage />}
    </div>
  );
}
