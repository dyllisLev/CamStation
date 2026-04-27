import { useState, useEffect } from 'react';
import { getSettings, updateSettings } from '../api/client';
import type { Settings } from '../types';

export function SettingsPage() {
  const [form, setForm] = useState<Settings>({
    retention_days: 30, segment_minutes: 10,
    motion_threshold: 0.02, max_storage_gb: 0,
  });
  const [saved, setSaved] = useState(false);

  useEffect(() => { getSettings().then(setForm).catch(console.error); }, []);

  const handleSave = async () => {
    await updateSettings(form);
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  const field = (label: string, key: keyof Settings, step = 1) => (
    <div style={{ marginBottom: 16 }}>
      <label style={{ display: 'block', fontSize: 12, color: '#aaa', marginBottom: 4 }}>{label}</label>
      <input
        type="number"
        step={step}
        value={form[key] as number}
        onChange={e => setForm(p => ({ ...p, [key]: Number(e.target.value) }))}
        style={{ background: '#2a2a2a', border: '1px solid #444', color: '#eee', borderRadius: 4, padding: '4px 8px', width: 120 }}
      />
    </div>
  );

  return (
    <div style={{ padding: 24, color: '#eee', maxWidth: 400 }}>
      <h2 style={{ marginBottom: 20, fontSize: 16 }}>설정</h2>
      {field('보존 기간 (일)', 'retention_days')}
      {field('세그먼트 길이 (분)', 'segment_minutes')}
      {field('모션 감도 임계값', 'motion_threshold', 0.01)}
      {field('최대 저장 용량 GB (0 = 무제한)', 'max_storage_gb')}
      <button onClick={handleSave} style={{ background: '#1565c0', border: 'none', color: '#fff', padding: '8px 20px', borderRadius: 4, cursor: 'pointer', marginTop: 8 }}>
        {saved ? '저장됨 ✓' : '저장'}
      </button>
    </div>
  );
}
