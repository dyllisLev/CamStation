# Windows Viewer Session Detection and Desktop Shortcut Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Install CamStation Viewer from local or RDP desktop sessions under the actual shell user and add a stable public-desktop shortcut.

**Architecture:** Replace the computer-wide CIM username lookup with the current Windows shell process token, keeping SID validation in a platform-neutral helper that can be exercised on Linux. Generate and remove one owned `.lnk` through the existing bounded PowerShell path; the shortcut runs the stable `CamStationViewer` scheduled task rather than a versioned Electron binary.

**Tech Stack:** Go 1.24, `golang.org/x/sys/windows`, Windows Task Scheduler, PowerShell 5+, Windows Script Host shortcut COM, existing transactional installer and release publisher.

## Global Constraints

- Select the user who owns the interactive shell in the installer's current local or RDP session.
- Never fall back to the UAC elevation account, another session, or an arbitrary logged-on account.
- Create exactly `CamStation Viewer.lnk` in Windows `CommonDesktopDirectory`.
- The shortcut runs `schtasks.exe /Run /TN "CamStationViewer"` and uses the stable bootstrap executable as its icon.
- Failed-install rollback, repair recovery, and uninstall must remove or restore the owned shortcut.
- Do not add an installer wizard, Start Menu entry, per-user shortcut, helper executable, or new dependency.
- Do not commit generated installers, embedded payload ZIPs, `data/`, logs, or runtime state.

---

## File Map

- Modify `internal/viewerinstall/registration.go`: add testable SID validation/lookup orchestration and bounded shortcut PowerShell script builders.
- Modify `internal/viewerinstall/windows.go`: read the current session shell process token, register the public shortcut, and remove it during unregister.
- Modify `internal/viewerinstall/registration_test.go`: cover shell SID selection and exact shortcut ownership/commands.
- Modify `docs/07-implementation-status.md`: record RDP-aware installation and the public shortcut.
- Generate but do not commit `viewer-app/dist/CamStationViewerSetup.exe` and `data/viewer-releases/**`.

### Task 1: Resolve the Interactive Shell User SID

**Files:**
- Modify: `internal/viewerinstall/registration.go`
- Modify: `internal/viewerinstall/windows.go`
- Test: `internal/viewerinstall/registration_test.go`

**Interfaces:**
- Consumes: `validTaskSID(string) bool` and `golang.org/x/sys/windows` process/token APIs.
- Produces: `interactiveShellSID(shellPID uint32, lookup func(uint32) (string, error)) (string, error)` and the existing `ActiveConsoleUserSID(context.Context) (string, error)` backed by the current shell token.

- [ ] **Step 1: Write failing shell-user selection tests**

Add to `internal/viewerinstall/registration_test.go`:

```go
func TestInteractiveShellSIDUsesShellProcessOwner(t *testing.T) {
	var selectedPID uint32
	sid, err := interactiveShellSID(77, func(pid uint32) (string, error) {
		selectedPID = pid
		return "S-1-5-21-1000", nil
	})
	if err != nil || sid != "S-1-5-21-1000" || selectedPID != 77 {
		t.Fatalf("sid=%q selectedPID=%d err=%v", sid, selectedPID, err)
	}
}

func TestInteractiveShellSIDRejectsMissingShellAndInvalidOwner(t *testing.T) {
	if _, err := interactiveShellSID(0, func(uint32) (string, error) {
		return "S-1-5-21-1000", nil
	}); err == nil || !strings.Contains(err.Error(), "interactive desktop session") {
		t.Fatalf("missing shell err=%v", err)
	}
	if _, err := interactiveShellSID(77, func(uint32) (string, error) {
		return "not-a-sid", nil
	}); err == nil || !strings.Contains(err.Error(), "SID is invalid") {
		t.Fatalf("invalid SID err=%v", err)
	}
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
go test ./internal/viewerinstall -run 'TestInteractiveShellSID' -count=1
```

Expected: FAIL to compile because `interactiveShellSID` is undefined.

- [ ] **Step 3: Add the smallest testable SID resolver**

Add to `internal/viewerinstall/registration.go` near `validTaskSID`:

```go
func interactiveShellSID(shellPID uint32, lookup func(uint32) (string, error)) (string, error) {
	if shellPID == 0 {
		return "", errors.New("interactive desktop session is required")
	}
	sid, err := lookup(shellPID)
	if err != nil {
		return "", fmt.Errorf("interactive shell user lookup failed: %w", err)
	}
	sid = strings.TrimSpace(sid)
	if !validTaskSID(sid) {
		return "", errors.New("interactive shell user SID is invalid")
	}
	return sid, nil
}
```

- [ ] **Step 4: Replace CIM lookup with the current shell process token**

Replace `ActiveConsoleUserSID` in `internal/viewerinstall/windows.go` and add the token helper:

```go
func ActiveConsoleUserSID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	shell := windows.GetShellWindow()
	if shell == 0 {
		return "", errors.New("interactive desktop session is required")
	}
	var processID uint32
	if _, err := windows.GetWindowThreadProcessId(shell, &processID); err != nil {
		return "", fmt.Errorf("interactive shell process lookup failed: %w", err)
	}
	return interactiveShellSID(processID, shellProcessUserSID)
}

func shellProcessUserSID(processID uint32) (string, error) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return "", err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}
```

Do not retain the `Get-CimInstance Win32_ComputerSystem` fallback.

- [ ] **Step 5: Verify GREEN and Windows compilation**

Run:

```bash
go test ./internal/viewerinstall -run 'TestInteractiveShellSID' -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/camstation-viewerinstall-windows.test.exe ./internal/viewerinstall
```

Expected: focused tests PASS and the Windows test executable is created without compiler errors.

- [ ] **Step 6: Commit the shell identity fix**

```bash
git add internal/viewerinstall/registration.go internal/viewerinstall/windows.go internal/viewerinstall/registration_test.go
git commit -m "fix(viewerinstall): select current desktop user"
```

### Task 2: Register and Remove the Public Desktop Shortcut

**Files:**
- Modify: `internal/viewerinstall/registration.go`
- Modify: `internal/viewerinstall/windows.go`
- Test: `internal/viewerinstall/registration_test.go`

**Interfaces:**
- Consumes: `ViewerTaskName`, `stableBootstrapPath(Layout)`, `Layout.InstallDir`, and `runWindows`.
- Produces: `publicDesktopShortcutScript(bootstrapPath, installDir string) (string, error)` and `removePublicDesktopShortcutScript() string`.

- [ ] **Step 1: Write failing shortcut script tests**

Add to `internal/viewerinstall/registration_test.go`:

```go
func TestPublicDesktopShortcutTargetsStableViewerTask(t *testing.T) {
	script, err := publicDesktopShortcutScript(
		`C:\Program Files\CamStation Viewer\CamStationViewerBootstrap.exe`,
		`C:\Program Files\CamStation Viewer`,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"CommonDesktopDirectory",
		"CamStation Viewer.lnk",
		"schtasks.exe",
		`/Run /TN "CamStationViewer"`,
		"CreateShortcut",
		"IconLocation",
		"WorkingDirectory",
	} {
		if !strings.Contains(script, required) {
			t.Fatalf("shortcut script missing %q: %s", required, script)
		}
	}
}

func TestPublicDesktopShortcutRemovalOwnsOnlyExactLink(t *testing.T) {
	script := removePublicDesktopShortcutScript()
	for _, required := range []string{"CommonDesktopDirectory", "CamStation Viewer.lnk", "-LiteralPath", "SilentlyContinue"} {
		if !strings.Contains(script, required) {
			t.Fatalf("removal script missing %q: %s", required, script)
		}
	}
	if strings.Contains(script, "*.lnk") {
		t.Fatalf("removal script uses a broad shortcut pattern: %s", script)
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```bash
go test ./internal/viewerinstall -run 'TestPublicDesktopShortcut' -count=1
```

Expected: FAIL to compile because both shortcut script functions are undefined.

- [ ] **Step 3: Add bounded shortcut script builders**

Add to `internal/viewerinstall/registration.go`:

```go
const viewerShortcutName = "CamStation Viewer.lnk"

func publicDesktopShortcutScript(bootstrapPath, installDir string) (string, error) {
	if !absolutePlatformPath(bootstrapPath) || !absolutePlatformPath(installDir) {
		return "", errors.New("invalid Viewer shortcut paths")
	}
	return `$desktop=[Environment]::GetFolderPath('CommonDesktopDirectory'); if(!$desktop){throw 'public desktop is unavailable'}; ` +
		`$path=[IO.Path]::Combine($desktop,'` + viewerShortcutName + `'); ` +
		`$link=(New-Object -ComObject WScript.Shell).CreateShortcut($path); ` +
		`$link.TargetPath=[IO.Path]::Combine($env:SystemRoot,'System32','schtasks.exe'); ` +
		`$link.Arguments='/Run /TN "` + ViewerTaskName + `"'; ` +
		`$link.IconLocation=$args[0]+',0'; $link.WorkingDirectory=$args[1]; ` +
		`$link.Description='CamStation Viewer'; $link.Save(); ` +
		`if(!(Test-Path -LiteralPath $path -PathType Leaf)){throw 'shortcut was not created'}`, nil
}

func removePublicDesktopShortcutScript() string {
	return `$desktop=[Environment]::GetFolderPath('CommonDesktopDirectory'); ` +
		`if($desktop){$path=[IO.Path]::Combine($desktop,'` + viewerShortcutName + `'); ` +
		`Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue}`
}
```

The only variable values inserted into script source are package constants. Pass filesystem paths as separate PowerShell arguments.

- [ ] **Step 4: Create the shortcut during runtime registration**

In `RegisterRuntime` in `internal/viewerinstall/windows.go`, after both scheduled tasks are created and before ACL validation, add:

```go
	shortcutScript, err := publicDesktopShortcutScript(stableBootstrapPath(layout), layout.InstallDir)
	if err != nil {
		return "", err
	}
	if _, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", shortcutScript, stableBootstrapPath(layout), layout.InstallDir); err != nil {
		return "", fmt.Errorf("register public desktop shortcut: %w", err)
	}
```

- [ ] **Step 5: Remove the shortcut during unregister**

In `UnregisterAll` in `internal/viewerinstall/windows.go`, append this exact cleanup function to `deletes` before calling `unregisterSequence`:

```go
	deletes = append(deletes, func(ctx context.Context) error {
		_, err := runWindows(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", removePublicDesktopShortcutScript())
		return err
	})
```

The existing clean-install compensation calls `Unregister`, while repair compensation calls `RegisterRuntime` again with the prior SID. Those existing paths therefore remove or recreate the owned shortcut without adding it to filesystem snapshot metadata.

- [ ] **Step 6: Verify GREEN and registration regressions**

Run:

```bash
go test ./internal/viewerinstall -run 'TestPublicDesktopShortcut|TestViewerLogonTask|TestUnregister' -count=1
go test ./internal/viewerinstall ./cmd/camstation-viewer-installer -count=1
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/camstation-viewer-installer-windows.test.exe ./cmd/camstation-viewer-installer
```

Expected: all selected tests PASS and the Windows installer test executable cross-compiles.

- [ ] **Step 7: Commit shortcut registration**

```bash
git add internal/viewerinstall/registration.go internal/viewerinstall/windows.go internal/viewerinstall/registration_test.go
git commit -m "feat(viewerinstall): add public desktop shortcut"
```

### Task 3: Verify, Build, and Publish the Test Installer

**Files:**
- Modify: `docs/07-implementation-status.md`
- Generate, do not commit: `viewer-app/dist/CamStationViewerSetup.exe`
- Generate, do not commit: `data/viewer-releases/**`

**Interfaces:**
- Consumes: `viewer-app/scripts/build-installer.mjs`, `scripts/publish-viewer-release.sh`, and the daemon Viewer release endpoints.
- Produces: a published unsigned development release `2.0.0-dev.2` downloadable from `/api/viewers/app/download`.

- [ ] **Step 1: Update implementation status**

Extend the Windows monitoring client delivery bullets in `docs/07-implementation-status.md` with:

```markdown
  - initial install binds the Viewer task to the actual shell user in the current local or RDP session, independent of UAC elevation credentials
  - install and repair create `CamStation Viewer.lnk` on the public desktop; it launches the stable scheduled task and uninstall or rollback removes it
```

- [ ] **Step 2: Run repository verification**

Run:

```bash
go test ./...
go build ./cmd/camstationd
cd viewer-app && npm test && npm run build
```

Expected: Go tests, daemon build, Viewer tests, and Viewer TypeScript build all PASS.

- [ ] **Step 3: Cross-compile every Windows Viewer entry point**

Run from the repository root:

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/CamStationViewerHost.exe ./cmd/camstation-viewer-host
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/CamStationViewerBootstrap.exe ./cmd/camstation-viewer-bootstrap
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/camstation-viewer-agent.exe ./cmd/camstation-viewer-agent
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/CamStationViewerSetup.exe ./cmd/camstation-viewer-installer
```

Expected: all four PE executables build without compiler errors.

- [ ] **Step 4: Build the embedded development installer**

Run from the repository root:

```bash
SERVER_URL="${CAMSTATION_VIEWER_SERVER_URL:-http://$(hostname -I | awk '{print $1}'):18080}"
cd viewer-app
npm run build:installer -- --server-url "$SERVER_URL" --version 2.0.0-dev.2
```

Expected: `viewer-app/dist/CamStationViewerSetup.exe` exists, begins with the Windows `MZ` header, and `cmd/camstation-viewer-installer/payload/release.zip` has been removed by the build cleanup.

- [ ] **Step 5: Publish the immutable release**

Run from the repository root:

```bash
scripts/publish-viewer-release.sh \
  --installer viewer-app/dist/CamStationViewerSetup.exe \
  --version 2.0.0-dev.2 \
  --release-dir data/viewer-releases \
  --development-unsigned
```

Expected: the publisher prints `published 2.0.0-dev.2`, creates a new immutable release directory named from version plus the calculated installer SHA-256, moves the former current pointer to previous, and changes no tracked file.

- [ ] **Step 6: Verify the served artifact**

Run:

```bash
curl -fsS http://127.0.0.1:18080/api/viewers/app/version
curl -fsS -o /tmp/CamStationViewerSetup-download.exe http://127.0.0.1:18080/api/viewers/app/download
sha256sum /tmp/CamStationViewerSetup-download.exe data/viewer-releases/current/CamStationViewerSetup.exe
```

Expected: metadata reports `2.0.0-dev.2`, both SHA-256 values are identical, and the download is the newly published installer.

- [ ] **Step 7: Commit status documentation**

```bash
git add docs/07-implementation-status.md
git commit -m "docs(viewer): record RDP install and desktop shortcut"
```

- [ ] **Step 8: Record the Windows acceptance check**

On the Windows test PC, run the new installer from an elevated PowerShell in the intended local or RDP desktop session. Confirm:

```powershell
Get-ScheduledTask -TaskName CamStationViewer | Select-Object TaskName, State
Get-Service CamStationViewerAgent | Select-Object Name, Status, StartType
$desktop = [Environment]::GetFolderPath('CommonDesktopDirectory')
Test-Path (Join-Path $desktop 'CamStation Viewer.lnk')
```

Expected: the task exists and runs, the Agent service is running and automatic, and the public shortcut exists. Launch the shortcut twice and verify Task Scheduler still reports one running `CamStationViewer` task. Uninstall from Windows Installed Apps and verify the shortcut is removed.
