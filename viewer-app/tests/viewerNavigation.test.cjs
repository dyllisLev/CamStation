const test = require('node:test');
const assert = require('node:assert/strict');

const {
  buildViewerUrl,
  normalizeServerUrl,
  shouldRestrictViewerNavigation,
} = require('../dist-electron/viewerNavigation.js');

test('서버 주소를 /new 신규 UI 라이브 전용 URL로 변환한다', () => {
  assert.equal(normalizeServerUrl(' http://10.0.0.26/ '), 'http://10.0.0.26');
  assert.equal(buildViewerUrl('http://10.0.0.26/'), 'http://10.0.0.26/new?viewer=1');
});

test('뷰어 앱에서는 기존 /viewer 및 /new 녹화·설정 이동을 제한한다', () => {
  const serverUrl = 'http://10.0.0.26';

  assert.equal(shouldRestrictViewerNavigation('http://10.0.0.26/viewer', serverUrl), true);
  assert.equal(shouldRestrictViewerNavigation('http://10.0.0.26/new/recordings', serverUrl), true);
  assert.equal(shouldRestrictViewerNavigation('http://10.0.0.26/new/settings', serverUrl), true);
  assert.equal(shouldRestrictViewerNavigation('http://10.0.0.26/new', serverUrl), false);
  assert.equal(shouldRestrictViewerNavigation('http://10.0.0.26/api/status', serverUrl), false);
  assert.equal(shouldRestrictViewerNavigation('http://example.com/new/settings', serverUrl), false);
});
