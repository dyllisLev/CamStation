import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const skillURL = new URL(
  "../../.agents/skills/verify-windows-viewer-gui/SKILL.md",
  import.meta.url,
);
const runbookURL = new URL(
  "../../.agents/skills/verify-windows-viewer-gui/references/evidence-loop.md",
  import.meta.url,
);
const metadataURL = new URL(
  "../../.agents/skills/verify-windows-viewer-gui/agents/openai.yaml",
  import.meta.url,
);

test("repository skill discovers CamStation Windows GUI verification requests", async () => {
  const [skill, metadata] = await Promise.all([
    readFile(skillURL, "utf8"),
    readFile(metadataURL, "utf8"),
  ]);

  assert.match(skill, /^---\nname: verify-windows-viewer-gui\n/u);
  assert.match(skill, /description:.*CamStation Viewer GUI.*Windows.*Linux\/SSH/iu);
  assert.match(skill, /launch, observe, capture, screenshot, visually inspect/iu);
  assert.match(skill, /rendering.*keyboard focus.*interactive Windows session/isu);
  assert.match(skill, /Windows GUI 캡처.*화면 직접 확인.*포커스 확인.*실행 화면 확인/isu);
  assert.match(metadata, /default_prompt: "Use \$verify-windows-viewer-gui/u);
  assert.doesNotMatch(
    skill + metadata,
    /(?:\d{1,3}\.){3}\d{1,3}|SHA256:[A-Za-z0-9+/]{20,}|BEGIN (?:OPENSSH|RSA|EC|DSA) PRIVATE KEY/u,
  );
});

test("repository skill preserves the bounded GUI evidence and cleanup contract", async () => {
  const [skill, runbook] = await Promise.all([
    readFile(skillURL, "utf8"),
    readFile(runbookURL, "utf8"),
  ]);
  const source = `${skill}\n${runbook}`;

  assert.match(source, /Invoke-CamStationViewerGuiCapture\.ps1/u);
  assert.match(source, /Capture-CamStationViewerWindow\.ps1/u);
  assert.match(source, /INTERACTIVE_GUI_CAPTURE_COMPLETE/u);
  assert.match(source, /TASK_LOGON_INTERACTIVE_TOKEN/u);
  assert.match(source, /viewer-window\.png.*uia\.json.*complete\.json/isu);
  assert.match(source, /SHA-256.*complete\.json/isu);
  assert.match(source, /view_image/u);
  assert.match(source, /Repeat `Capture` after the renderer settles/u);
  assert.match(source, /TaskDeleted.*zero.*task.*zero.*worker/isu);
  assert.match(source, /Never substitute a full-desktop\s+screenshot/u);
  assert.match(source, /never collect an edit control's value/iu);
  assert.match(source, /Do not install VNC, AnyDesk, RustDesk/iu);
  assert.match(source, /Do not add a listener, firewall rule, account, stored credential/iu);
  assert.match(source, /weakened Viewer named-pipe ACL/iu);
});
