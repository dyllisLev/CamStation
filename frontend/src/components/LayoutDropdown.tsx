import { useState, useRef, useEffect } from 'react';
import type { LayoutProfile } from '../types';

interface Props {
  layouts: LayoutProfile[];
  currentId: string | null;
  isDirty: boolean;
  onLoad: (id: string) => void;
  onSave: () => void;
  onSaveAs: (name: string) => void;
  onCancel: () => void;
  onDelete: (id: string) => void;
}

export function LayoutDropdown({
  layouts, currentId, isDirty, onLoad, onSave, onSaveAs, onCancel, onDelete,
}: Props) {
  const [open, setOpen] = useState(false);
  const [saveAsMode, setSaveAsMode] = useState(false);
  const [newName, setNewName] = useState('');
  const ref = useRef<HTMLDivElement>(null);

  const current = layouts.find(l => l.id === currentId);
  const displayName = current?.name ?? '저장 안됨';

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
        setSaveAsMode(false);
        setNewName('');
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const handleSaveAs = () => {
    if (!newName.trim()) return;
    onSaveAs(newName.trim());
    setNewName('');
    setSaveAsMode(false);
    setOpen(false);
  };

  return (
    <div ref={ref} style={{ position: 'relative', display: 'flex', alignItems: 'center', gap: 4 }}>
      <button
        onClick={() => setOpen(o => !o)}
        style={{
          background: '#1a1a2e', border: '1px solid #444', borderRadius: 5,
          padding: '3px 8px', color: '#eee', fontSize: 12, cursor: 'pointer',
          display: 'flex', alignItems: 'center', gap: 5,
        }}
      >
        {displayName}
        {isDirty && <span style={{ color: '#ffa726', fontSize: 11 }}>편집됨</span>}
        <span style={{ color: '#666', fontSize: 10 }}>▾</span>
      </button>

      {isDirty && (
        <>
          <button
            onClick={onSave}
            title="저장"
            style={{
              width: 24, height: 24, borderRadius: '50%', border: 'none',
              background: '#1e88e5', color: '#fff', fontSize: 14, cursor: 'pointer',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}
          >✓</button>
          <button
            onClick={onCancel}
            title="취소"
            style={{
              width: 24, height: 24, borderRadius: '50%', border: 'none',
              background: '#424242', color: '#eee', fontSize: 14, cursor: 'pointer',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}
          >↩</button>
        </>
      )}

      {open && (
        <div style={{
          position: 'absolute', top: '100%', left: 0, marginTop: 4,
          background: '#1e1e2e', border: '1px solid #333', borderRadius: 6,
          minWidth: 190, zIndex: 100, padding: 6,
        }}>
          {layouts.length === 0 && (
            <div style={{ color: '#555', fontSize: 11, padding: '4px 8px' }}>저장된 레이아웃 없음</div>
          )}
          {layouts.map(l => (
            <div
              key={l.id}
              style={{
                display: 'flex', alignItems: 'center', padding: '5px 8px',
                borderRadius: 4,
                background: l.id === currentId ? '#1a3a5a' : 'transparent',
                color: l.id === currentId ? '#64b5f6' : '#ccc',
              }}
            >
              <span
                style={{ flex: 1, fontSize: 12, cursor: 'pointer' }}
                onClick={() => { onLoad(l.id); setOpen(false); }}
              >
                {l.name}
              </span>
              <button
                onClick={() => onDelete(l.id)}
                style={{
                  background: 'none', border: 'none', color: '#555',
                  cursor: 'pointer', fontSize: 12, padding: '0 2px',
                }}
              >✕</button>
            </div>
          ))}

          <hr style={{ border: 'none', borderTop: '1px solid #2a2a3a', margin: '6px 0' }} />

          {saveAsMode ? (
            <div style={{ display: 'flex', gap: 4, padding: '2px 4px' }}>
              <input
                autoFocus
                value={newName}
                onChange={e => setNewName(e.target.value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') handleSaveAs();
                  if (e.key === 'Escape') { setSaveAsMode(false); setNewName(''); }
                }}
                placeholder="레이아웃 이름"
                style={{
                  flex: 1, background: '#0f0f1f', border: '1px solid #444',
                  borderRadius: 3, color: '#eee', fontSize: 11, padding: '3px 6px',
                }}
              />
              <button
                onClick={handleSaveAs}
                disabled={!newName.trim()}
                style={{
                  background: '#1e88e5', border: 'none', borderRadius: 3,
                  color: '#fff', fontSize: 11, padding: '2px 8px', cursor: 'pointer',
                }}
              >저장</button>
            </div>
          ) : (
            <button
              onClick={() => setSaveAsMode(true)}
              style={{
                width: '100%', background: 'none', border: '1px dashed #444',
                borderRadius: 4, color: '#666', fontSize: 11,
                padding: '5px', cursor: 'pointer',
              }}
            >+ 새 레이아웃으로 저장</button>
          )}
        </div>
      )}
    </div>
  );
}
