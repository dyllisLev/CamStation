import assert from "node:assert/strict";
import test from "node:test";
import { nextSetupState, setupErrorMessage } from "../src/setupModel.ts";

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
  assert.equal(setupErrorMessage("unknown raw server response"), "설정을 저장할 수 없습니다.");
});
