/* global electronAPI */

const urlInput     = document.getElementById('urlInput');
const testBtn      = document.getElementById('testBtn');
const startBtn     = document.getElementById('startBtn');
const statusEl     = document.getElementById('status');
const fullscreenEl = document.getElementById('fullscreen');
const versionEl    = document.getElementById('version');

function normalize(raw) {
  return raw.trim().replace(/\/+$/, '');
}

// 저장된 설정 및 앱 버전 로드
window.electronAPI.getSettings().then(({ serverUrl, fullscreenOnStart }) => {
  if (serverUrl) {
    urlInput.value = serverUrl;
    startBtn.disabled = false;
  }
  fullscreenEl.checked = fullscreenOnStart;
});

window.electronAPI.getVersion().then(v => {
  versionEl.textContent = `v${v}`;
});

// 연결 테스트
testBtn.addEventListener('click', async () => {
  const url = normalize(urlInput.value);
  if (!url) return;
  statusEl.textContent = '연결 중...';
  statusEl.className = '';
  testBtn.disabled = true;

  const result = await window.electronAPI.testConnection(url);
  testBtn.disabled = false;

  if (result.ok) {
    statusEl.textContent = '✓ 연결됨';
    statusEl.className = 'ok';
    await window.electronAPI.saveSettings({
      serverUrl: url,
      fullscreenOnStart: fullscreenEl.checked,
    });
    startBtn.disabled = false;
  } else {
    statusEl.textContent = `✗ ${result.error || '연결 실패'}`;
    statusEl.className = 'err';
    startBtn.disabled = true;
  }
});

// 시작
startBtn.addEventListener('click', async () => {
  const url = normalize(urlInput.value);
  await window.electronAPI.saveSettings({
    serverUrl: url,
    fullscreenOnStart: fullscreenEl.checked,
  });
  await window.electronAPI.launchViewer(url);
});

// Enter 키 → 연결 테스트
urlInput.addEventListener('keydown', e => {
  if (e.key === 'Enter') testBtn.click();
});
