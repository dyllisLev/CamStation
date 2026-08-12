import assert from "node:assert/strict";
import test from "node:test";
import { nextSetupState, setupErrorMessage, setupHydration } from "../src/setupModel.ts";

test("failed replacement keeps the active server while preserving the entered draft", () => {
  const previous = {
    draft: { serverUrl: "http://old.example", displayName: "기존", autoStart: true },
    activeConfig: { serverUrl: "http://old.example", displayName: "기존" },
  };
  const draft = { serverUrl: "http://new.example", displayName: "새 이름", autoStart: false };

  assert.deepEqual(nextSetupState(previous, draft, "server_unreachable"), {
    draft,
    activeConfig: previous.activeConfig,
    errorCode: "server_unreachable",
  });
});

test("setup errors are Korean, distinct, and do not expose raw service text", () => {
  assert.equal(setupErrorMessage("invalid_input"), "입력값을 확인해 주세요.");
  assert.equal(setupErrorMessage("server_unreachable"), "서버에 연결할 수 없습니다.");
  assert.equal(setupErrorMessage("api_incompatible"), "서버 버전이 호환되지 않습니다.");
  assert.equal(setupErrorMessage("registration_rejected"), "Viewer 등록이 거부되었습니다.");
  assert.equal(setupErrorMessage("service_unavailable"), "관리 서비스에 연결할 수 없습니다.");
  assert.equal(setupErrorMessage("unknown raw server response"), "설정을 저장할 수 없습니다.");
});

test("delayed setup state never replaces a field the operator is editing", () => {
  const status = {
    config: { serverUrl: "http://old.example", displayName: "기존 Viewer" },
    autoStart: true,
    connection: "offline",
  };

  assert.deepEqual(setupHydration(status, { dirty: true, editing: true }), {
    focusServer: false,
  });
  assert.deepEqual(setupHydration(status, { dirty: false, editing: true }), {
    focusServer: false,
  });
});

test("untouched setup state hydrates once and focuses the server field", () => {
  assert.deepEqual(setupHydration({
    config: { serverUrl: "http://server.example:18080", displayName: "관제실" },
    autoStart: false,
    connection: "offline",
  }, { dirty: false, editing: false }), {
    draft: {
      serverUrl: "http://server.example:18080",
      displayName: "관제실",
      autoStart: false,
    },
    focusServer: true,
  });

  assert.deepEqual(setupHydration({
    autoStart: true,
    connection: "service_unavailable",
  }, { dirty: false, editing: false }), {
    draft: { serverUrl: "", displayName: "", autoStart: true },
    focusServer: true,
    message: "관리 서비스에 연결할 수 없습니다.",
  });
});
