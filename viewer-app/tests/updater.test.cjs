const test = require('node:test');
const assert = require('node:assert/strict');

const {
  buildWindowsUpdateScript,
  resolvePortableExePath,
  shouldInstallViewerUpdate,
} = require('../dist-electron/updater.js');

test('업데이트 대상 버전이 현재 버전과 같거나 이미 대기 중이면 설치하지 않는다', () => {
  assert.equal(shouldInstallViewerUpdate('1.0.1', '1.0.1', null), false);
  assert.equal(shouldInstallViewerUpdate('1.0.1', '1.0.2', '1.0.2'), false);
  assert.equal(shouldInstallViewerUpdate('1.0.1', '1.0.2', null), true);
});

test('포터블 EXE 원본 경로가 있으면 임시 실행 경로 대신 그 경로를 교체 대상으로 사용한다', () => {
  assert.equal(
    resolvePortableExePath({ PORTABLE_EXECUTABLE_FILE: 'C:\\CamViewer\\CamViewer.exe' }, 'C:\\Temp\\CamViewer.exe'),
    'C:\\CamViewer\\CamViewer.exe',
  );
  assert.equal(
    resolvePortableExePath({}, 'C:\\Temp\\CamViewer.exe'),
    'C:\\Temp\\CamViewer.exe',
  );
});

test('윈도우 업데이트 스크립트는 실행 중인 EXE 종료를 기다린 뒤 새 파일로 교체하고 자동 재실행한다', () => {
  const script = buildWindowsUpdateScript('C:\\Temp\\CamViewer-new.exe', 'C:\\CamViewer\\CamViewer.exe');

  assert.match(script, /timeout \/t 2 \/nobreak > nul/);
  assert.match(script, /move \/y "C:\\Temp\\CamViewer-new.exe" "C:\\CamViewer\\CamViewer.exe"/);
  assert.match(script, /start "" "C:\\CamViewer\\CamViewer.exe"/);
  assert.match(script, /del "%~f0"/);
});
