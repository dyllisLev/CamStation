import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const skillRoot = new URL(
  "../../.agents/skills/control-camstation-windows-pc/",
  import.meta.url,
);
const skillURL = new URL("SKILL.md", skillRoot);
const controlRunbookURL = new URL("references/control-plan.md", skillRoot);
const targetsRunbookURL = new URL("references/targets.md", skillRoot);
const systemRunbookURL = new URL("references/system-control.md", skillRoot);
const setupRunbookURL = new URL("references/setup.md", skillRoot);
const viewerRunbookURL = new URL("references/evidence-loop.md", skillRoot);
const metadataURL = new URL("agents/openai.yaml", skillRoot);

test("one repository skill discovers setup, general WinPC control, and Viewer requests", async () => {
  const [skill, metadata] = await Promise.all([
    readFile(skillURL, "utf8"),
    readFile(metadataURL, "utf8"),
  ]);

  assert.match(skill, /^---\nname: control-camstation-windows-pc\n/u);
  assert.match(skill, /Install, audit, observe, and control.*CamStation Windows test PC or monitoring PC/iu);
  assert.match(skill, /screenshots.*app launch\/close.*click.*typing.*hotkeys.*scroll/iu);
  assert.match(skill, /move\/resize\/maximize\/fullscreen.*Viewer.*GUI capture/iu);
  assert.match(skill, /WinPC 제어.*테스트 PC 조작.*모니터링 PC 제어.*전체 화면 캡처.*창 최대화.*전체화면/isu);
  assert.match(metadata, /default_prompt: "Use \$control-camstation-windows-pc/u);
  assert.doesNotMatch(
    skill + metadata,
    /(?:\d{1,3}\.){3}\d{1,3}|SHA256:[A-Za-z0-9+/]{20,}|BEGIN (?:OPENSSH|RSA|EC|DSA) PRIVATE KEY/u,
  );
});

test("the skill requires explicit target selection, one standardized path, and verified cleanup", async () => {
  const [skill, targets, control, system, setup] = await Promise.all([
    readFile(skillURL, "utf8"),
    readFile(targetsRunbookURL, "utf8"),
    readFile(controlRunbookURL, "utf8"),
    readFile(systemRunbookURL, "utf8"),
    readFile(setupRunbookURL, "utf8"),
  ]);
  const source = `${skill}\n${targets}\n${control}\n${system}\n${setup}`;

  assert.match(source, /Invoke-CamStationWindowsTarget\.mjs.*only Linux-side/isu);
  assert.match(source, /There is no default target/iu);
  assert.match(source, /test-pc.*monitoring-pc/isu);
  assert.match(source, /computer name.*maintenance.*interactive.*session ID/isu);
  assert.match(source, /status.*system.*plan.*desktop-capture.*viewer-capture.*cleanup/isu);
  assert.match(source, /one bounded Cua\/UIA batch.*observation.*actions.*post-action observations/isu);
  assert.match(source, /UTF-8\s+stdin/iu);
  assert.match(source, /Every mutating tool requires `verifyWith`/u);
  assert.match(source, /effect=unverifiable.*not proof/isu);
  assert.match(source, /SHA-256.*complete\.json/isu);
  assert.match(source, /view_image/u);
  assert.match(source, /TaskDeleted=true/iu);
  assert.match(source, /Do not add a listener, firewall rule, account, stored credential/iu);
  assert.match(source, /Do not add a listener, firewall rule, account, stored credential/iu);
  assert.match(source, /background UIA.*foreground.*fresh post/isu);
  assert.match(source, /element_index.*not `index`/isu);
  assert.match(source, /exact PID.*Never use.*broad/isu);
  assert.match(source, /Windows has no Unix zombie state/iu);
  assert.match(source, /pinned.*archive SHA-256.*six-file.*each installed file SHA-256/isu);
  assert.match(source, /TemporarySetupTaskCount=0/u);
  assert.match(source, /NotSigned.*signed software/isu);
  assert.match(source, /Never paste.*schtasks.*EncodedCommand/isu);
  assert.doesNotMatch(
    source,
    /viewer2-transition|clean Viewer 2\.0 transition|viewer2-operational|CamViewer 1\.0\.4/iu,
  );
  assert.doesNotMatch(
    source,
    /(?:\d{1,3}\.){3}\d{1,3}|SHA256:[A-Za-z0-9+/]{20,}|BEGIN (?:OPENSSH|RSA|EC|DSA) PRIVATE KEY/u,
  );
});

test("ViewerCapture mode preserves the exact-window evidence contract", async () => {
  const viewer = await readFile(viewerRunbookURL, "utf8");

  assert.match(viewer, /Invoke-CamStationViewerGuiCapture\.ps1/u);
  assert.match(viewer, /Capture-CamStationViewerWindow\.ps1/u);
  assert.match(viewer, /INTERACTIVE_GUI_CAPTURE_COMPLETE/u);
  assert.match(viewer, /viewer-window\.png.*uia\.json.*complete\.json/isu);
  assert.match(viewer, /Repeat `Capture` after the\s+renderer settles/u);
  assert.match(viewer, /TaskDeleted=true.*zero.*task.*zero.*worker/isu);
  assert.match(viewer, /Never substitute a full-desktop\s+screenshot/u);
  assert.match(viewer, /Never collect an edit control's\s+value/iu);
  assert.match(viewer, /preserves a maximized window.*fails closed.*minimized/isu);
});
