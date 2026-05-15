import { useState, useEffect, useCallback } from 'react';
import { getSettings, updateSettings, getStorageStats, getSystemVersion, triggerUpdate, getViewerVersion, getCameraConfig, updateCameraEnabled } from '../api/client';
import type { Settings, StorageStats, SystemVersion, CameraConfigStatus } from '../types';

export function SettingsPage() {
  const [form, setForm] = useState<Settings>({
    retention_days: 30, segment_minutes: 10,
    motion_threshold: 0.02, max_storage_gb: 0,
    motion_enabled: true,
  });
  const [saved, setSaved] = useState(false);
  const [stats, setStats] = useState<StorageStats | null>(null);
  const [statsLoading, setStatsLoading] = useState(true);
  const [version, setVersion] = useState<SystemVersion | null>(null);
  const [updateLoading, setUpdateLoading] = useState(false);
  const [updateMsg, setUpdateMsg] = useState<string | null>(null);
  const [viewerVersion, setViewerVersion] = useState<string | null>(null);
  const [cameraConfig, setCameraConfig] = useState<CameraConfigStatus[]>([]);
  const [cameraConfigLoading, setCameraConfigLoading] = useState(true);
  const [cameraToggling, setCameraToggling] = useState<string | null>(null);
  const [cameraMsg, setCameraMsg] = useState<string | null>(null);

  useEffect(() => { getSettings().then(setForm).catch(console.error); }, []);

  useEffect(() => {
    getStorageStats()
      .then(setStats)
      .catch(console.error)
      .finally(() => setStatsLoading(false));
  }, []);

  const loadVersion = useCallback(() => {
    getSystemVersion().then(setVersion).catch(console.error);
  }, []);

  useEffect(() => { loadVersion(); }, [loadVersion]);

  useEffect(() => {
    getViewerVersion().then(v => setViewerVersion(v.version)).catch(() => {});
  }, []);

  const loadCameraConfig = useCallback(() => {
    setCameraConfigLoading(true);
    getCameraConfig()
      .then(setCameraConfig)
      .catch(err => {
        console.error(err);
        setCameraMsg('카메라 설정을 불러오지 못했습니다.');
      })
      .finally(() => setCameraConfigLoading(false));
  }, []);

  useEffect(() => { loadCameraConfig(); }, [loadCameraConfig]);

  const handleToggleCamera = async (camera: CameraConfigStatus) => {
    const nextEnabled = !camera.enabled;
    setCameraToggling(camera.id);
    setCameraMsg(null);
    try {
      const updated = await updateCameraEnabled(camera.id, nextEnabled);
      setCameraConfig(prev => prev.map(item => item.id === updated.id ? updated : item));
      setCameraMsg(`${camera.id} ${nextEnabled ? '활성화' : '비활성화'} 완료. 뷰어에 자동 반영됩니다.`);
    } catch (err) {
      console.error(err);
      setCameraMsg(`${camera.id} ${nextEnabled ? '활성화' : '비활성화'} 실패.`);
    } finally {
      setCameraToggling(null);
    }
  };

  const handleSave = async () => {
    await updateSettings(form);
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  const handleUpdate = async () => {
    setUpdateLoading(true);
    setUpdateMsg(null);
    const startingVersion = version?.current_version;
    try {
      const res = await triggerUpdate();
      if (res.status === 'already_running') {
        setUpdateMsg('이미 업데이트 진행 중입니다.');
        setUpdateLoading(false);
        return;
      }
      setUpdateMsg('업데이트 중... 완료되면 자동으로 새로고침됩니다.');
      const deadline = Date.now() + 3 * 60 * 1000;
      const poll = async () => {
        if (Date.now() > deadline) {
          setUpdateMsg('업데이트 시간 초과. 페이지를 수동으로 새로고침하세요.');
          setUpdateLoading(false);
          return;
        }
        try {
          const v = await getSystemVersion();
          if (v.current_version !== startingVersion) {
            window.location.reload();
            return;
          }
        } catch {
          // 서버 재시작 중일 수 있음, 계속 폴링
        }
        setTimeout(poll, 3000);
      };
      setTimeout(poll, 5000);
    } catch {
      setUpdateMsg('업데이트 요청 실패.');
      setUpdateLoading(false);
    }
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

  const diskPct = stats ? (stats.disk_used_gb / stats.disk_total_gb) * 100 : 0;
  const diskColor = diskPct > 90 ? '#ef5350' : diskPct > 75 ? '#ffa726' : '#42a5f5';

  return (
    <div style={{ padding: 24, color: '#eee', maxWidth: 600 }}>
      <h2 style={{ marginBottom: 20, fontSize: 16 }}>설정</h2>

      {/* Storage Stats */}
      <div style={{ marginBottom: 28, background: '#1e1e1e', border: '1px solid #333', borderRadius: 6, padding: 16 }}>
        <div style={{ fontSize: 13, fontWeight: 'bold', marginBottom: 12, color: '#90caf9' }}>저장소 현황</div>

        {statsLoading ? (
          <div style={{ fontSize: 12, color: '#666' }}>불러오는 중...</div>
        ) : stats ? (
          <>
            {/* Disk usage bar */}
            <div style={{ marginBottom: 12 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: '#aaa', marginBottom: 4 }}>
                <span>디스크 사용량</span>
                <span style={{ color: diskColor }}>
                  {stats.disk_used_gb.toFixed(1)} GB / {stats.disk_total_gb.toFixed(1)} GB ({diskPct.toFixed(0)}%)
                </span>
              </div>
              <div style={{ background: '#333', borderRadius: 3, height: 8, overflow: 'hidden' }}>
                <div style={{ width: `${Math.min(diskPct, 100)}%`, height: '100%', background: diskColor, borderRadius: 3, transition: 'width 0.3s' }} />
              </div>
              <div style={{ fontSize: 11, color: '#666', marginTop: 3 }}>
                녹화 데이터: {stats.recordings_gb.toFixed(1)} GB &nbsp;·&nbsp; 여유: {stats.disk_free_gb.toFixed(1)} GB
              </div>
            </div>

            {/* Per-camera table */}
            <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 11 }}>
              <thead>
                <tr style={{ color: '#777', borderBottom: '1px solid #333' }}>
                  <th style={{ textAlign: 'left', padding: '4px 6px', fontWeight: 'normal' }}>카메라</th>
                  <th style={{ textAlign: 'right', padding: '4px 6px', fontWeight: 'normal' }}>총 용량</th>
                  <th style={{ textAlign: 'right', padding: '4px 6px', fontWeight: 'normal' }}>시간당</th>
                  <th style={{ textAlign: 'right', padding: '4px 6px', fontWeight: 'normal' }}>일/기간</th>
                  <th style={{ textAlign: 'right', padding: '4px 6px', fontWeight: 'normal' }}>가장 오래된 날짜</th>
                </tr>
              </thead>
              <tbody>
                {[...stats.cameras].sort((a, b) => b.total_gb - a.total_gb).map(cam => (
                  <tr key={cam.camera_id} style={{ borderBottom: '1px solid #2a2a2a' }}>
                    <td style={{ padding: '5px 6px', color: '#ddd' }}>{cam.camera_id}</td>
                    <td style={{ padding: '5px 6px', textAlign: 'right', color: '#eee' }}>{cam.total_gb.toFixed(1)} GB</td>
                    <td style={{ padding: '5px 6px', textAlign: 'right', color: '#81c784' }}>{cam.hourly_gb >= 1 ? cam.hourly_gb.toFixed(2) : (cam.hourly_gb * 1024).toFixed(0) + ' MB'}/h</td>
                    <td style={{ padding: '5px 6px', textAlign: 'right', color: '#aaa' }}>{cam.days_recorded}일</td>
                    <td style={{ padding: '5px 6px', textAlign: 'right', color: '#666' }}>{cam.oldest_date ?? '-'}</td>
                  </tr>
                ))}
              </tbody>
              <tfoot>
                <tr style={{ borderTop: '1px solid #444', color: '#aaa' }}>
                  <td style={{ padding: '5px 6px' }}>합계</td>
                  <td style={{ padding: '5px 6px', textAlign: 'right', color: '#eee' }}>{stats.recordings_gb.toFixed(1)} GB</td>
                  <td style={{ padding: '5px 6px', textAlign: 'right', color: '#81c784' }}>
                    {stats.hourly_gb_total >= 1 ? stats.hourly_gb_total.toFixed(2) : (stats.hourly_gb_total * 1024).toFixed(0) + ' MB'}/h
                  </td>
                  <td colSpan={2} />
                </tr>
              </tfoot>
            </table>

            {/* Daily projection */}
            <div style={{ marginTop: 8, fontSize: 11, color: '#666', borderTop: '1px solid #2a2a2a', paddingTop: 8 }}>
              하루 예상 사용량: ~{(stats.hourly_gb_total * 24).toFixed(1)} GB/일
              {form.max_storage_gb > 0 && (
                <span style={{ marginLeft: 12 }}>
                  · 현재 용량으로 약 {Math.floor(form.max_storage_gb / (stats.hourly_gb_total * 24))}일치 보관 가능
                </span>
              )}
            </div>
          </>
        ) : (
          <div style={{ fontSize: 12, color: '#ef5350' }}>통계를 불러올 수 없습니다.</div>
        )}
      </div>

      {/* Camera Enablement */}
      <div style={{ marginBottom: 28, background: '#1e1e1e', border: '1px solid #333', borderRadius: 6, padding: 16 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
          <div style={{ fontSize: 13, fontWeight: 'bold', color: '#90caf9' }}>카메라 활성화</div>
          <button
            onClick={loadCameraConfig}
            disabled={cameraConfigLoading || cameraToggling !== null}
            style={{ background: '#2a2a2a', border: '1px solid #444', color: '#bbb', padding: '4px 10px', borderRadius: 4, cursor: 'pointer', fontSize: 11 }}
          >
            새로고침
          </button>
        </div>
        <div style={{ fontSize: 11, color: '#777', marginBottom: 10 }}>
          비활성화된 카메라는 화면·녹화·상태 알림 대상에서 제외됩니다.
        </div>
        {cameraConfigLoading ? (
          <div style={{ fontSize: 12, color: '#666' }}>불러오는 중...</div>
        ) : cameraConfig.length === 0 ? (
          <div style={{ fontSize: 12, color: '#666' }}>카메라 설정이 없습니다.</div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {cameraConfig.map(camera => {
              const busy = cameraToggling === camera.id;
              return (
                <div key={camera.id} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, borderBottom: '1px solid #2a2a2a', paddingBottom: 8 }}>
                  <div>
                    <div style={{ fontSize: 12, color: camera.enabled ? '#eee' : '#777', fontWeight: 600 }}>{camera.name}</div>
                    <div style={{ fontSize: 11, color: '#666', marginTop: 2 }}>
                      {camera.enabled ? (camera.online ? '온라인' : '오프라인') : '비활성화'} · 보조 스트림 {camera.has_sub ? '있음' : '없음'}
                    </div>
                  </div>
                  <button
                    onClick={() => handleToggleCamera(camera)}
                    disabled={cameraToggling !== null}
                    style={{
                      minWidth: 82,
                      background: camera.enabled ? '#4e342e' : '#1b5e20',
                      border: `1px solid ${camera.enabled ? '#8d6e63' : '#388e3c'}`,
                      color: '#fff',
                      padding: '6px 12px',
                      borderRadius: 4,
                      cursor: cameraToggling === null ? 'pointer' : 'default',
                      fontSize: 12,
                      opacity: cameraToggling !== null && !busy ? 0.45 : 1,
                    }}
                  >
                    {busy ? '처리 중...' : camera.enabled ? '비활성화' : '활성화'}
                  </button>
                </div>
              );
            })}
          </div>
        )}
        {cameraMsg && <div style={{ marginTop: 10, fontSize: 11, color: cameraMsg.includes('실패') ? '#ef5350' : '#81c784' }}>{cameraMsg}</div>}
      </div>

      {/* Settings form */}
      <div style={{ marginBottom: 16 }}>
        <label style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 12, color: '#aaa', cursor: 'pointer' }}>
          <input
            type="checkbox"
            checked={form.motion_enabled}
            onChange={e => setForm(p => ({ ...p, motion_enabled: e.target.checked }))}
            style={{ width: 16, height: 16, cursor: 'pointer' }}
          />
          모션 감지 활성화
        </label>
      </div>
      {field('보존 기간 (일)', 'retention_days')}
      {field('세그먼트 길이 (분)', 'segment_minutes')}
      {field('모션 감도 임계값', 'motion_threshold', 0.01)}
      <div style={{ marginBottom: 16 }}>
        <label style={{ display: 'block', fontSize: 12, color: '#aaa', marginBottom: 4 }}>
          자동 삭제 용량 한도 GB <span style={{ color: '#666' }}>(0 = 사용 안 함)</span>
        </label>
        <input
          type="number"
          step={1}
          value={form.max_storage_gb}
          onChange={e => setForm(p => ({ ...p, max_storage_gb: Number(e.target.value) }))}
          style={{ background: '#2a2a2a', border: '1px solid #444', color: '#eee', borderRadius: 4, padding: '4px 8px', width: 120 }}
        />
        {form.max_storage_gb > 0 && stats && stats.disk_used_gb > form.max_storage_gb * 0.9 && (
          <div style={{ fontSize: 11, color: '#ffa726', marginTop: 4 }}>
            현재 녹화 용량({stats.recordings_gb.toFixed(1)} GB)이 한도({form.max_storage_gb} GB)에 근접했습니다. 다음 정리 시 오래된 영상이 삭제됩니다.
          </div>
        )}
      </div>
      <button onClick={handleSave} style={{ background: '#1565c0', border: 'none', color: '#fff', padding: '8px 20px', borderRadius: 4, cursor: 'pointer', marginTop: 8 }}>
        {saved ? '저장됨 ✓' : '저장'}
      </button>

      {/* System Update */}
      <div style={{ marginTop: 32, background: '#1e1e1e', border: '1px solid #333', borderRadius: 6, padding: 16 }}>
        <div style={{ fontSize: 13, fontWeight: 'bold', marginBottom: 12, color: '#90caf9' }}>시스템 업데이트</div>
        {version ? (
          <>
            <div style={{ fontSize: 12, color: '#aaa', marginBottom: 4 }}>
              현재 버전: <span style={{ color: '#eee' }}>{version.current_version}</span>
            </div>
            <div style={{ fontSize: 12, color: '#aaa', marginBottom: 12 }}>
              최신 버전:{' '}
              <span style={{ color: version.update_available ? '#81c784' : '#eee' }}>
                {version.latest_version ?? '확인 불가'}
              </span>
              {version.update_available && (
                <span style={{ marginLeft: 8, color: '#81c784', fontSize: 11 }}>업데이트 있음</span>
              )}
            </div>
            <button
              onClick={handleUpdate}
              disabled={updateLoading || !version.update_available}
              style={{
                background: version.update_available ? '#1565c0' : '#333',
                border: 'none',
                color: version.update_available ? '#fff' : '#666',
                padding: '6px 16px',
                borderRadius: 4,
                cursor: version.update_available ? 'pointer' : 'default',
                fontSize: 12,
              }}
            >
              {updateLoading ? '업데이트 중...' : '지금 업데이트'}
            </button>
            {updateMsg && (
              <div style={{ marginTop: 8, fontSize: 11, color: '#ffa726' }}>{updateMsg}</div>
            )}
          </>
        ) : (
          <div style={{ fontSize: 12, color: '#666' }}>버전 정보를 불러오는 중...</div>
        )}
      </div>

      {/* Viewer App */}
      <div style={{ marginTop: 32, background: '#1e1e1e', border: '1px solid #333', borderRadius: 6, padding: 16 }}>
        <div style={{ fontSize: 13, fontWeight: 'bold', marginBottom: 12, color: '#90caf9' }}>뷰어 앱 (Windows)</div>
        <div style={{ fontSize: 12, color: '#aaa', marginBottom: 12 }}>
          {viewerVersion
            ? <>버전: <span style={{ color: '#eee' }}>{viewerVersion}</span></>
            : <span style={{ color: '#666' }}>배포된 뷰어 없음</span>
          }
        </div>
        <a
          href="/api/settings/viewer-app"
          download="CamViewer.exe"
          style={{
            display: 'inline-block',
            background: viewerVersion ? '#1565c0' : '#333',
            color: viewerVersion ? '#fff' : '#555',
            padding: '6px 16px',
            borderRadius: 4,
            fontSize: 12,
            textDecoration: 'none',
            pointerEvents: viewerVersion ? 'auto' : 'none',
          }}
        >
          CamViewer.exe 다운로드
        </a>
      </div>
    </div>
  );
}
