const test = require('node:test');
const assert = require('node:assert/strict');

const {
  shouldInstallUpdate,
  buildWindowsPortableUpdateScript,
  normalizeVersion,
} = require('../dist-electron/updaterCore.js');

test('서버 버전이 현재 버전보다 다를 때만 업데이트를 설치한다', () => {
  assert.equal(shouldInstallUpdate('1.0.2', '1.0.1', null), true);
  assert.equal(shouldInstallUpdate(' 1.0.2\n', '1.0.1', null), true);
  assert.equal(shouldInstallUpdate('1.0.1', '1.0.1', null), false);
  assert.equal(shouldInstallUpdate('1.0.2', '1.0.1', '1.0.2'), false);
});

test('빈 버전 문자열은 업데이트 대상으로 보지 않는다', () => {
  assert.equal(normalizeVersion(' 1.0.2\n'), '1.0.2');
  assert.equal(shouldInstallUpdate('', '1.0.1', null), false);
  assert.equal(shouldInstallUpdate('   ', '1.0.1', null), false);
});

test('포터블 EXE 교체 스크립트는 실행 중 파일 교체 후 자동 재실행한다', () => {
  const script = buildWindowsPortableUpdateScript({
    newExePath: 'C:\\Temp\\CamViewer-new.exe',
    exePath: 'C:\\Apps\\CamViewer.exe',
  });

  assert.match(script, /timeout \/t 2 \/nobreak > nul/);
  assert.match(script, /move \/y "C:\\Temp\\CamViewer-new\.exe" "C:\\Apps\\CamViewer\.exe"/);
  assert.match(script, /start "" "C:\\Apps\\CamViewer\.exe"/);
  assert.match(script, /del "%~f0"/);
});
