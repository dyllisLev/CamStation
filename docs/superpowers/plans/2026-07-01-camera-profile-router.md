# Camera Profile Router Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a camera-aware registration and stream routing layer so CamStation scans current cameras, stores recording/live stream roles, records from main streams, and plays live views from sub streams.

**Architecture:** Add a camera profile package that normalizes Tapo ONVIF, Reolink API, and Generic RTSP discovery into stream candidates. Extend the SQLite store with role-specific `camera_streams`, generate go2rtc streams per role, and update recorder/live consumers to use explicit role stream names while preserving stable camera layout keys.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite`, React 19, TanStack Query, React Router 7, Vite, lucide-react, existing go2rtc and ffmpeg process management.

## Global Constraints

- Target only current cameras in v1: TP-Link Tapo C320WS (`집-창고1`, `집-창고2`, `집-마당`) and Reolink Duo WiFi channels (`소방서3`, `소방서4`).
- A camera remains one user-facing camera in UI, timeline, layouts, events, and recording pages.
- Recording uses the high-quality/main stream by default.
- Live monitoring uses the low-bandwidth/sub stream by default.
- Scanning and profile matching are read-only; do not change camera encoder settings.
- Database is the source of truth; go2rtc config and ffmpeg commands are generated artifacts.
- Redact credentials in API responses, events, logs, docs, and diagnostics.
- Preserve existing layout identity: the legacy `streamName` remains the camera key; playback uses `liveStreamName`.
- Use `scripts/camstationctl.sh` for runtime restart/verify; do not hand-roll process killing.
- Existing dirty worktree changes may be present; do not revert unrelated files.

---

## File Structure

- Create `internal/cameraprofile/types.go`: normalized profile scanner types, stream roles, candidate metadata, redaction helpers for scan reports.
- Create `internal/cameraprofile/tapo.go`: Tapo C320WS ONVIF response parsing and default role selection.
- Create `internal/cameraprofile/reolink.go`: Reolink Duo WiFi API response parsing and default role selection.
- Create `internal/cameraprofile/generic.go`: Generic RTSP fallback adapter.
- Create `internal/cameraprofile/scanner.go`: read-only scanner orchestration with injectable HTTP/ONVIF/RTSP clients for tests.
- Create `internal/cameraprofile/*_test.go`: parser, mapper, and scanner unit tests.
- Modify `internal/store/store.go`: add camera metadata fields, `camera_streams` table, stream role CRUD, migration backfill.
- Create `internal/store/store_test.go`: migration and stream role persistence tests.
- Modify `internal/stream/go2rtc.go`: generate go2rtc config from role streams, not a single camera URL.
- Modify `internal/stream/go2rtc_test.go`: verify role stream config and runtime parsing.
- Modify `internal/recorder/recorder.go`: reconcile and start workers from recording role streams.
- Modify `internal/recorder/recorder_test.go`: verify recording role input and camera-name archive behavior.
- Modify `cmd/camstationd/main.go`: add scan API, update camera save API, expose role fields, use role streams for restart/reconcile/timeline translation.
- Modify `cmd/camstationd/main_test.go`: route/API regression tests and frontend source guards.
- Modify `web/src/app/api.ts`: new camera stream/profile types and scan/create payloads.
- Modify `web/src/app/queries.ts`: camera scan mutation and updated create camera mutation.
- Modify `web/src/components/live/LiveWorkspace.tsx`: use camera key for layout identity and `liveStreamName` for playback.
- Modify `web/src/pages/ControlRoomPage.tsx`: preview uses `liveStreamName`; table shows recording/live role states.
- Modify `web/src/pages/CamerasPage.tsx`: replace RTSP form with registration wizard and profile settings.
- Modify `web/src/styles/index.css`: wizard/profile setting styles matching existing console UI.
- Modify `docs/07-implementation-status.md`: update implemented/partial status after runtime verification.
- Modify `cmd/camstationd/web/*`: regenerated frontend build output after frontend changes.

---

### Task 1: Store Camera Stream Roles

**Files:**
- Modify: `internal/store/store.go`
- Create: `internal/store/store_test.go`

**Interfaces:**
- Produces:
  - `type CameraStreamRole string`
  - constants `CameraStreamRoleRecording`, `CameraStreamRoleLive`, `CameraStreamRoleSnapshot`
  - `type CameraStream struct`
  - `func (d *DB) ReplaceCameraStreams(ctx context.Context, cameraID int64, streams []CameraStream) error`
  - `func (d *DB) ListCameraStreams(ctx context.Context, cameraID int64, includeSecrets bool) ([]CameraStream, error)`
  - `func (d *DB) ListAllCameraStreams(ctx context.Context, includeSecrets bool) ([]CameraStream, error)`
  - `func (c Camera) StreamForRole(role CameraStreamRole) (CameraStream, bool)`
- Consumes: existing `store.Camera`, `DB.Migrate`, `redactCameraURL`.

- [ ] **Step 1: Write the failing store migration test**

Create `internal/store/store_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func TestCameraStreamRolesPersistAndRedact(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	camera, err := db.UpsertCamera(ctx, Camera{
		Name:       "소방서3",
		URL:        "rtsp://user:pass@192.168.0.12:554/h264Preview_01_main",
		StreamName: "3",
		State:      "streaming",
	})
	if err != nil {
		t.Fatalf("upsert camera: %v", err)
	}

	streams := []CameraStream{
		{
			CameraID:         camera.ID,
			Role:             CameraStreamRoleRecording,
			Label:            "main",
			Source:           "reolink-api",
			URL:              "rtsp://user:pass@192.168.0.12:554/h264Preview_01_main",
			Go2RTCStreamName: "3-recording",
			Codec:            "h264",
			Width:            2560,
			Height:           1440,
			FPS:              15,
			BitrateKbps:      3072,
			State:            "streaming",
		},
		{
			CameraID:         camera.ID,
			Role:             CameraStreamRoleLive,
			Label:            "sub",
			Source:           "reolink-api",
			URL:              "rtsp://user:pass@192.168.0.12:554/h264Preview_01_sub",
			Go2RTCStreamName: "3-live",
			Codec:            "h264",
			Width:            640,
			Height:           360,
			FPS:              10,
			BitrateKbps:      256,
			State:            "streaming",
		},
	}
	if err := db.ReplaceCameraStreams(ctx, camera.ID, streams); err != nil {
		t.Fatalf("replace streams: %v", err)
	}

	public, err := db.ListCameras(ctx, false)
	if err != nil {
		t.Fatalf("list cameras: %v", err)
	}
	if len(public) != 1 {
		t.Fatalf("camera count = %d, want 1", len(public))
	}
	got := public[0]
	if got.StreamName != "3" {
		t.Fatalf("legacy camera key = %q, want 3", got.StreamName)
	}
	if got.RecordingStreamName != "3-recording" {
		t.Fatalf("recording stream = %q, want 3-recording", got.RecordingStreamName)
	}
	if got.LiveStreamName != "3-live" {
		t.Fatalf("live stream = %q, want 3-live", got.LiveStreamName)
	}
	if got.URL != "" {
		t.Fatalf("public camera URL leaked: %q", got.URL)
	}
	if len(got.Streams) != 2 {
		t.Fatalf("stream count = %d, want 2", len(got.Streams))
	}
	for _, stream := range got.Streams {
		if stream.URL != "" {
			t.Fatalf("public stream URL leaked for %s: %q", stream.Role, stream.URL)
		}
		if stream.RedactedURL == "" || stream.RedactedURL == stream.URL {
			t.Fatalf("stream %s missing redacted URL", stream.Role)
		}
	}

	private, err := db.ListCameras(ctx, true)
	if err != nil {
		t.Fatalf("list private cameras: %v", err)
	}
	if private[0].Streams[0].URL == "" {
		t.Fatalf("private stream URL should be available to runtime code")
	}
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
go test ./internal/store -run TestCameraStreamRolesPersistAndRedact -count=1
```

Expected: FAIL with undefined `CameraStream`, `CameraStreamRoleRecording`, `ReplaceCameraStreams`, or missing camera fields.

- [ ] **Step 3: Add store types and schema**

Modify `internal/store/store.go`:

```go
type CameraStreamRole string

const (
	CameraStreamRoleRecording CameraStreamRole = "recording"
	CameraStreamRoleLive      CameraStreamRole = "live"
	CameraStreamRoleSnapshot  CameraStreamRole = "snapshot"
)

type CameraStream struct {
	ID               int64            `json:"id"`
	CameraID         int64            `json:"camera_id"`
	Role             CameraStreamRole `json:"role"`
	Label            string           `json:"label"`
	Source           string           `json:"source"`
	URL              string           `json:"url,omitempty"`
	RedactedURL      string           `json:"redactedUrl"`
	Go2RTCStreamName string           `json:"go2rtcStreamName"`
	Codec            string           `json:"codec,omitempty"`
	Width            int              `json:"width,omitempty"`
	Height           int              `json:"height,omitempty"`
	FPS              float64          `json:"fps,omitempty"`
	BitrateKbps      int              `json:"bitrateKbps,omitempty"`
	ProfileToken     string           `json:"profileToken,omitempty"`
	State            string           `json:"state"`
	LastProbeJSON    map[string]any   `json:"lastProbe,omitempty"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}
```

Extend `Camera`:

```go
type Camera struct {
	ID                  int64          `json:"id"`
	Name                string         `json:"name"`
	URL                 string         `json:"url,omitempty"`
	RedactedURL         string         `json:"redactedUrl"`
	StreamName          string         `json:"streamName"`
	LayoutKey           string         `json:"layoutKey"`
	RecordingStreamName string         `json:"recordingStreamName,omitempty"`
	LiveStreamName      string         `json:"liveStreamName,omitempty"`
	Manufacturer        string         `json:"manufacturer,omitempty"`
	Model               string         `json:"model,omitempty"`
	ProfileAdapter      string         `json:"profileAdapter,omitempty"`
	Host                string         `json:"host,omitempty"`
	RTSPPort            int            `json:"rtspPort,omitempty"`
	HTTPPort            int            `json:"httpPort,omitempty"`
	ONVIFPort           int            `json:"onvifPort,omitempty"`
	ChannelIndex        *int           `json:"channelIndex,omitempty"`
	State               string         `json:"state"`
	LastScanJSON        map[string]any `json:"lastScan,omitempty"`
	LastProbeJSON       map[string]any `json:"lastProbe,omitempty"`
	Streams             []CameraStream `json:"streams,omitempty"`
	CreatedAt           time.Time      `json:"createdAt"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}
```

Add migration statements in `Migrate` after the existing `cameras` table creation:

```go
`ALTER TABLE cameras ADD COLUMN manufacturer TEXT NOT NULL DEFAULT ''`,
`ALTER TABLE cameras ADD COLUMN model TEXT NOT NULL DEFAULT ''`,
`ALTER TABLE cameras ADD COLUMN profile_adapter TEXT NOT NULL DEFAULT ''`,
`ALTER TABLE cameras ADD COLUMN host TEXT NOT NULL DEFAULT ''`,
`ALTER TABLE cameras ADD COLUMN rtsp_port INTEGER NOT NULL DEFAULT 0`,
`ALTER TABLE cameras ADD COLUMN http_port INTEGER NOT NULL DEFAULT 0`,
`ALTER TABLE cameras ADD COLUMN onvif_port INTEGER NOT NULL DEFAULT 0`,
`ALTER TABLE cameras ADD COLUMN channel_index INTEGER`,
`ALTER TABLE cameras ADD COLUMN last_scan_json TEXT NOT NULL DEFAULT '{}'`,
`CREATE TABLE IF NOT EXISTS camera_streams (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	camera_id INTEGER NOT NULL,
	role TEXT NOT NULL,
	label TEXT NOT NULL,
	source TEXT NOT NULL,
	url TEXT NOT NULL,
	go2rtc_stream_name TEXT NOT NULL UNIQUE,
	codec TEXT NOT NULL DEFAULT '',
	width INTEGER NOT NULL DEFAULT 0,
	height INTEGER NOT NULL DEFAULT 0,
	fps REAL NOT NULL DEFAULT 0,
	bitrate_kbps INTEGER NOT NULL DEFAULT 0,
	profile_token TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'unknown',
	last_probe_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(camera_id, role),
	FOREIGN KEY(camera_id) REFERENCES cameras(id) ON DELETE CASCADE
)`,
`CREATE INDEX IF NOT EXISTS idx_camera_streams_camera_role
	ON camera_streams(camera_id, role)`,
```

Because SQLite errors if an added column already exists, wrap `ALTER TABLE ... ADD COLUMN` statements with a helper:

```go
func ignoreDuplicateColumn(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "duplicate column name") {
		return nil
	}
	return err
}
```

Use it only for `ALTER TABLE` statements in `Migrate`.

- [ ] **Step 4: Add stream persistence helpers**

Add to `internal/store/store.go`:

```go
func (d *DB) ReplaceCameraStreams(ctx context.Context, cameraID int64, streams []CameraStream) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM camera_streams WHERE camera_id = ?`, cameraID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, stream := range streams {
		if stream.CameraID == 0 {
			stream.CameraID = cameraID
		}
		if stream.State == "" {
			stream.State = "unknown"
		}
		probe := stream.LastProbeJSON
		if probe == nil {
			probe = map[string]any{}
		}
		encoded, err := json.Marshal(probe)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO camera_streams(
			camera_id, role, label, source, url, go2rtc_stream_name, codec,
			width, height, fps, bitrate_kbps, profile_token, state,
			last_probe_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			stream.CameraID, string(stream.Role), stream.Label, stream.Source,
			stream.URL, stream.Go2RTCStreamName, stream.Codec, stream.Width,
			stream.Height, stream.FPS, stream.BitrateKbps, stream.ProfileToken,
			stream.State, string(encoded), now, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) ListCameraStreams(ctx context.Context, cameraID int64, includeSecrets bool) ([]CameraStream, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id, camera_id, role, label, source, url,
		go2rtc_stream_name, codec, width, height, fps, bitrate_kbps, profile_token,
		state, last_probe_json, created_at, updated_at
		FROM camera_streams WHERE camera_id = ?
		ORDER BY CASE role WHEN 'recording' THEN 1 WHEN 'live' THEN 2 WHEN 'snapshot' THEN 3 ELSE 4 END`, cameraID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCameraStreams(rows, includeSecrets)
}

func (d *DB) ListAllCameraStreams(ctx context.Context, includeSecrets bool) ([]CameraStream, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id, camera_id, role, label, source, url,
		go2rtc_stream_name, codec, width, height, fps, bitrate_kbps, profile_token,
		state, last_probe_json, created_at, updated_at
		FROM camera_streams ORDER BY camera_id, role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCameraStreams(rows, includeSecrets)
}
```

Also add:

```go
func (c Camera) StreamForRole(role CameraStreamRole) (CameraStream, bool) {
	for _, stream := range c.Streams {
		if stream.Role == role {
			return stream, true
		}
	}
	return CameraStream{}, false
}
```

- [ ] **Step 5: Attach streams to listed cameras**

Modify `ListCameras` to call `ListCameraStreams` for each scanned camera. Set:

```go
camera.LayoutKey = camera.StreamName
if stream, ok := camera.StreamForRole(CameraStreamRoleRecording); ok {
	camera.RecordingStreamName = stream.Go2RTCStreamName
}
if stream, ok := camera.StreamForRole(CameraStreamRoleLive); ok {
	camera.LiveStreamName = stream.Go2RTCStreamName
} else {
	camera.LiveStreamName = camera.StreamName
}
```

For existing cameras with no role streams, preserve compatibility:

```go
if len(camera.Streams) == 0 && camera.URL != "" && camera.StreamName != "" {
	camera.Streams = []CameraStream{{
		CameraID:         camera.ID,
		Role:             CameraStreamRoleRecording,
		Label:            "legacy",
		Source:           "legacy",
		URL:              camera.URL,
		RedactedURL:      redactCameraURL(camera.URL),
		Go2RTCStreamName: camera.StreamName,
		State:            camera.State,
	}}
	camera.RecordingStreamName = camera.StreamName
	camera.LiveStreamName = camera.StreamName
}
```

- [ ] **Step 6: Run store tests**

Run:

```bash
go test ./internal/store -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "Add camera stream role storage"
```

---

### Task 2: Profile Adapter Parsers And Role Mapper

**Files:**
- Create: `internal/cameraprofile/types.go`
- Create: `internal/cameraprofile/tapo.go`
- Create: `internal/cameraprofile/reolink.go`
- Create: `internal/cameraprofile/generic.go`
- Create: `internal/cameraprofile/profile_test.go`

**Interfaces:**
- Produces:
  - `type StreamRole string`
  - `type StreamCandidate struct`
  - `type DeviceProfile struct`
  - `type RoleAssignment struct`
  - `func SelectDefaultRoles(profile DeviceProfile) RoleAssignment`
  - `func ParseTapoONVIF(deviceXML, profilesXML string) (DeviceProfile, error)`
  - `func ParseReolink(deviceJSON, channelJSON string, encByChannel map[int]string, host string, rtspPort int) (DeviceProfile, error)`
  - `func GenericRTSPProfile(name, host, rawURL string) DeviceProfile`
- Consumes: `store.CameraStreamRole` only in later tasks, not in this package.

- [ ] **Step 1: Write failing parser tests**

Create `internal/cameraprofile/profile_test.go`:

```go
package cameraprofile

import "testing"

func TestParseTapoONVIFSelectsMainMinorAndJPEG(t *testing.T) {
	t.Parallel()

	device := `<tds:GetDeviceInformationResponse>
<tds:Manufacturer>tp-link</tds:Manufacturer>
<tds:Model>Tapo C320WS</tds:Model>
<tds:FirmwareVersion>1.4.2 Build 250725 Rel.72234n</tds:FirmwareVersion>
</tds:GetDeviceInformationResponse>`
	profiles := `<trt:Profiles fixed="true" token="profile_1"><tt:Name>mainStream</tt:Name><tt:VideoEncoderConfiguration token="main"><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>2560</tt:Width><tt:Height>1440</tt:Height></tt:Resolution><tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>2048</tt:BitrateLimit></tt:RateControl></tt:VideoEncoderConfiguration></trt:Profiles>
<trt:Profiles fixed="true" token="profile_2"><tt:Name>minorStream</tt:Name><tt:VideoEncoderConfiguration token="minor"><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution><tt:RateControl><tt:FrameRateLimit>15</tt:FrameRateLimit><tt:BitrateLimit>256</tt:BitrateLimit></tt:RateControl></tt:VideoEncoderConfiguration></trt:Profiles>
<trt:Profiles fixed="true" token="profile_3"><tt:Name>jpegStream</tt:Name><tt:VideoEncoderConfiguration token="jpeg"><tt:Encoding>JPEG</tt:Encoding><tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution><tt:RateControl><tt:FrameRateLimit>1</tt:FrameRateLimit><tt:BitrateLimit>512</tt:BitrateLimit></tt:RateControl></tt:VideoEncoderConfiguration></trt:Profiles>`

	profile, err := ParseTapoONVIF(device, profiles)
	if err != nil {
		t.Fatalf("parse tapo profile: %v", err)
	}
	if profile.Adapter != "tplink-tapo-c320ws" {
		t.Fatalf("adapter = %q, want tplink-tapo-c320ws", profile.Adapter)
	}
	if profile.Manufacturer != "tp-link" || profile.Model != "Tapo C320WS" {
		t.Fatalf("identity = %s/%s", profile.Manufacturer, profile.Model)
	}
	roles := SelectDefaultRoles(profile)
	if roles.Recording.ProfileID != "mainStream" {
		t.Fatalf("recording profile = %q, want mainStream", roles.Recording.ProfileID)
	}
	if roles.Live.ProfileID != "minorStream" {
		t.Fatalf("live profile = %q, want minorStream", roles.Live.ProfileID)
	}
	if roles.Snapshot == nil || roles.Snapshot.ProfileID != "jpegStream" {
		t.Fatalf("snapshot profile = %#v, want jpegStream", roles.Snapshot)
	}
}

func TestParseReolinkDuoBuildsChannelMainAndSub(t *testing.T) {
	t.Parallel()

	device := `[{"cmd":"GetDevInfo","code":0,"value":{"DevInfo":{"model":"Reolink Duo WiFi","type":"MULTI_IPC","channelNum":2,"firmVer":"v3.0.0.684_21110101"}}}]`
	channels := `[{"cmd":"GetChannelstatus","code":0,"value":{"count":2,"status":[{"channel":0,"name":"1","online":1,"typeInfo":"Reolink Duo WiFi"},{"channel":1,"name":"2","online":1,"typeInfo":"Reolink Duo WiFi"}]}}]`
	enc := map[int]string{
		0: `[{"cmd":"GetEnc","code":0,"value":{"Enc":{"channel":0,"mainStream":{"bitRate":3072,"frameRate":15,"height":1440,"profile":"High","size":"2560*1440","width":2560},"subStream":{"bitRate":256,"frameRate":10,"height":360,"profile":"High","size":"640*360","width":640}}}}]`,
		1: `[{"cmd":"GetEnc","code":0,"value":{"Enc":{"channel":1,"mainStream":{"bitRate":3072,"frameRate":15,"height":1440,"profile":"High","size":"2560*1440","width":2560},"subStream":{"bitRate":256,"frameRate":10,"height":360,"profile":"High","size":"640*360","width":640}}}}]`,
	}

	profile, err := ParseReolink(device, channels, enc, "192.168.0.12", 554)
	if err != nil {
		t.Fatalf("parse reolink profile: %v", err)
	}
	if profile.Adapter != "reolink-duo-wifi" {
		t.Fatalf("adapter = %q, want reolink-duo-wifi", profile.Adapter)
	}
	if len(profile.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(profile.Channels))
	}
	channel1 := profile.Channels[1]
	roles := SelectDefaultRoles(DeviceProfile{Adapter: profile.Adapter, Channels: []ChannelProfile{channel1}})
	if roles.Recording.URL != "rtsp://192.168.0.12:554/h264Preview_02_main" {
		t.Fatalf("channel 1 recording URL = %q", roles.Recording.URL)
	}
	if roles.Live.URL != "rtsp://192.168.0.12:554/h264Preview_02_sub" {
		t.Fatalf("channel 1 live URL = %q", roles.Live.URL)
	}
}
```

- [ ] **Step 2: Run parser tests and verify failure**

Run:

```bash
go test ./internal/cameraprofile -count=1
```

Expected: FAIL because package files and functions do not exist.

- [ ] **Step 3: Add shared types**

Create `internal/cameraprofile/types.go`:

```go
package cameraprofile

type StreamRole string

const (
	RoleRecording StreamRole = "recording"
	RoleLive      StreamRole = "live"
	RoleSnapshot  StreamRole = "snapshot"
)

type StreamCandidate struct {
	ProfileID   string     `json:"profileId"`
	Label       string     `json:"label"`
	RoleHint    StreamRole `json:"roleHint"`
	Source      string     `json:"source"`
	URL         string     `json:"url,omitempty"`
	RedactedURL string     `json:"redactedUrl,omitempty"`
	Codec       string     `json:"codec,omitempty"`
	Width       int        `json:"width,omitempty"`
	Height      int        `json:"height,omitempty"`
	FPS         float64    `json:"fps,omitempty"`
	BitrateKbps int        `json:"bitrateKbps,omitempty"`
}

type ChannelProfile struct {
	Index      int               `json:"index"`
	Name       string            `json:"name"`
	Online     bool              `json:"online"`
	Candidates []StreamCandidate `json:"candidates"`
}

type DeviceProfile struct {
	Adapter      string           `json:"adapter"`
	Manufacturer string           `json:"manufacturer"`
	Model        string           `json:"model"`
	Firmware     string           `json:"firmware,omitempty"`
	Host         string           `json:"host"`
	RTSPPort     int              `json:"rtspPort,omitempty"`
	HTTPPort     int              `json:"httpPort,omitempty"`
	ONVIFPort    int              `json:"onvifPort,omitempty"`
	Channels     []ChannelProfile `json:"channels"`
	Warnings     []string         `json:"warnings,omitempty"`
}

type RoleAssignment struct {
	Recording StreamCandidate
	Live      StreamCandidate
	Snapshot  *StreamCandidate
}

func SelectDefaultRoles(profile DeviceProfile) RoleAssignment {
	var out RoleAssignment
	if len(profile.Channels) == 0 {
		return out
	}
	for _, candidate := range profile.Channels[0].Candidates {
		switch candidate.RoleHint {
		case RoleRecording:
			if out.Recording.ProfileID == "" {
				out.Recording = candidate
			}
		case RoleLive:
			if out.Live.ProfileID == "" {
				out.Live = candidate
			}
		case RoleSnapshot:
			if out.Snapshot == nil {
				copy := candidate
				out.Snapshot = &copy
			}
		}
	}
	if out.Live.ProfileID == "" {
		out.Live = out.Recording
	}
	return out
}
```

- [ ] **Step 4: Add Tapo parser**

Create `internal/cameraprofile/tapo.go`:

```go
package cameraprofile

import (
	"encoding/xml"
	"regexp"
	"strconv"
	"strings"
)

func ParseTapoONVIF(deviceXML, profilesXML string) (DeviceProfile, error) {
	profile := DeviceProfile{
		Adapter:      "tplink-tapo-c320ws",
		Manufacturer: findXMLText(deviceXML, "Manufacturer"),
		Model:        findXMLText(deviceXML, "Model"),
		Firmware:     findXMLText(deviceXML, "FirmwareVersion"),
		ONVIFPort:    2020,
		RTSPPort:     554,
	}
	blocks := regexp.MustCompile(`(?s)<trt:Profiles[^>]*token="([^"]+)"[^>]*>(.*?)</trt:Profiles>`).FindAllStringSubmatch(profilesXML, -1)
	channel := ChannelProfile{Index: 0, Name: "default", Online: true}
	for _, block := range blocks {
		token := block[1]
		body := block[2]
		name := findXMLText(body, "Name")
		candidate := StreamCandidate{
			ProfileID:   name,
			Label:       name,
			Source:      "onvif",
			Codec:       strings.ToLower(findXMLText(body, "Encoding")),
			Width:       atoi(findXMLText(body, "Width")),
			Height:      atoi(findXMLText(body, "Height")),
			FPS:         float64(atoi(findXMLText(body, "FrameRateLimit"))),
			BitrateKbps: atoi(findXMLText(body, "BitrateLimit")),
		}
		if candidate.ProfileID == "" {
			candidate.ProfileID = token
		}
		switch name {
		case "mainStream":
			candidate.RoleHint = RoleRecording
			candidate.URL = "rtsp://{host}:554/stream1"
		case "minorStream":
			candidate.RoleHint = RoleLive
			candidate.URL = "rtsp://{host}:554/stream2"
		case "jpegStream":
			candidate.RoleHint = RoleSnapshot
			candidate.URL = "rtsp://{host}:554/stream8"
		}
		channel.Candidates = append(channel.Candidates, candidate)
	}
	profile.Channels = []ChannelProfile{channel}
	return profile, nil
}

func findXMLText(raw, local string) string {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != local {
			continue
		}
		var value string
		if err := decoder.DecodeElement(&value, &start); err != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
}

func atoi(value string) int {
	number, _ := strconv.Atoi(strings.TrimSpace(value))
	return number
}
```

- [ ] **Step 5: Add Reolink parser**

Create `internal/cameraprofile/reolink.go` with JSON structs for the observed responses and this exported function:

```go
func ParseReolink(deviceJSON, channelJSON string, encByChannel map[int]string, host string, rtspPort int) (DeviceProfile, error) {
	if rtspPort == 0 {
		rtspPort = 554
	}
	profile := DeviceProfile{
		Adapter:      "reolink-duo-wifi",
		Manufacturer: "Reolink",
		Host:         host,
		RTSPPort:     rtspPort,
		HTTPPort:     443,
	}
	var dev []struct {
		Value struct {
			DevInfo struct {
				Model    string `json:"model"`
				FirmVer  string `json:"firmVer"`
				Channels int    `json:"channelNum"`
			} `json:"DevInfo"`
		} `json:"value"`
	}
	if err := json.Unmarshal([]byte(deviceJSON), &dev); err != nil {
		return DeviceProfile{}, err
	}
	if len(dev) > 0 {
		profile.Model = dev[0].Value.DevInfo.Model
		profile.Firmware = dev[0].Value.DevInfo.FirmVer
	}
	var channels []struct {
		Value struct {
			Status []struct {
				Channel int    `json:"channel"`
				Name    string `json:"name"`
				Online  int    `json:"online"`
			} `json:"status"`
		} `json:"value"`
	}
	if err := json.Unmarshal([]byte(channelJSON), &channels); err != nil {
		return DeviceProfile{}, err
	}
	for _, item := range channels[0].Value.Status {
		channel := ChannelProfile{Index: item.Channel, Name: item.Name, Online: item.Online == 1}
		encJSON := encByChannel[item.Channel]
		main, sub, err := parseReolinkEnc(encJSON, host, rtspPort, item.Channel)
		if err != nil {
			return DeviceProfile{}, err
		}
		channel.Candidates = []StreamCandidate{main, sub}
		profile.Channels = append(profile.Channels, channel)
	}
	return profile, nil
}
```

Add the helper used by `ParseReolink`:

```go
type reolinkEncResponse []struct {
	Value struct {
		Enc struct {
			Channel int `json:"channel"`
			MainStream struct {
				BitRate   int    `json:"bitRate"`
				FrameRate int    `json:"frameRate"`
				Height    int    `json:"height"`
				Profile   string `json:"profile"`
				Width     int    `json:"width"`
			} `json:"mainStream"`
			SubStream struct {
				BitRate   int    `json:"bitRate"`
				FrameRate int    `json:"frameRate"`
				Height    int    `json:"height"`
				Profile   string `json:"profile"`
				Width     int    `json:"width"`
			} `json:"subStream"`
		} `json:"Enc"`
	} `json:"value"`
}

func parseReolinkEnc(raw, host string, rtspPort, channel int) (StreamCandidate, StreamCandidate, error) {
	var payload reolinkEncResponse
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return StreamCandidate{}, StreamCandidate{}, err
	}
	if len(payload) == 0 {
		return StreamCandidate{}, StreamCandidate{}, fmt.Errorf("empty Reolink encoding response")
	}
	number := channel + 1
	main := payload[0].Value.Enc.MainStream
	sub := payload[0].Value.Enc.SubStream
	mainURL := fmt.Sprintf("rtsp://%s:%d/h264Preview_%02d_main", host, rtspPort, number)
	subURL := fmt.Sprintf("rtsp://%s:%d/h264Preview_%02d_sub", host, rtspPort, number)
	return StreamCandidate{
		ProfileID:   fmt.Sprintf("channel-%d-main", channel),
		Label:       "main",
		RoleHint:    RoleRecording,
		Source:      "reolink-api",
		URL:         mainURL,
		Codec:       "h264",
		Width:       main.Width,
		Height:      main.Height,
		FPS:         float64(main.FrameRate),
		BitrateKbps: main.BitRate,
	}, StreamCandidate{
		ProfileID:   fmt.Sprintf("channel-%d-sub", channel),
		Label:       "sub",
		RoleHint:    RoleLive,
		Source:      "reolink-api",
		URL:         subURL,
		Codec:       "h264",
		Width:       sub.Width,
		Height:      sub.Height,
		FPS:         float64(sub.FrameRate),
		BitrateKbps: sub.BitRate,
	}, nil
}
```

- [ ] **Step 6: Add generic fallback**

Create `internal/cameraprofile/generic.go`:

```go
package cameraprofile

func GenericRTSPProfile(name, host, rawURL string) DeviceProfile {
	candidate := StreamCandidate{
		ProfileID: "manual",
		Label:     "manual RTSP",
		RoleHint:  RoleRecording,
		Source:    "manual",
		URL:       rawURL,
	}
	return DeviceProfile{
		Adapter:      "generic-rtsp",
		Manufacturer: "Generic",
		Model:        "RTSP",
		Host:         host,
		RTSPPort:     554,
		Channels: []ChannelProfile{{
			Index:      0,
			Name:       name,
			Online:     true,
			Candidates: []StreamCandidate{candidate},
		}},
	}
}
```

- [ ] **Step 7: Run profile tests**

Run:

```bash
go test ./internal/cameraprofile -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/cameraprofile
git commit -m "Add camera profile adapters"
```

---

### Task 3: Read-Only Camera Scanner API Core

**Files:**
- Create: `internal/cameraprofile/scanner.go`
- Create: `internal/cameraprofile/scanner_test.go`

**Interfaces:**
- Consumes: Task 2 `DeviceProfile`, `ParseTapoONVIF`, `ParseReolink`, `GenericRTSPProfile`.
- Produces:
  - `type ScanRequest struct`
  - `type Scanner struct`
  - `type ScannerClient interface`
  - `func NewScanner(client ScannerClient) *Scanner`
  - `func NewNetworkScannerClient() ScannerClient` fallback stub for compile-time use; Task 9 replaces it with real network behavior.
  - `func (s *Scanner) Scan(ctx context.Context, req ScanRequest) (DeviceProfile, error)`

- [ ] **Step 1: Write scanner tests with fake clients**

Create `internal/cameraprofile/scanner_test.go`:

```go
package cameraprofile

import (
	"context"
	"testing"
)

type fakeScannerClient struct {
	ports       map[int]bool
	onvifDevice string
	onvifMedia  string
	reolinkDev  string
	reolinkChan string
	reolinkEnc  map[int]string
}

func (f fakeScannerClient) PortOpen(_ context.Context, _ string, port int) bool { return f.ports[port] }
func (f fakeScannerClient) ONVIFDeviceInfo(_ context.Context, _ ScanRequest) (string, error) {
	return f.onvifDevice, nil
}
func (f fakeScannerClient) ONVIFProfiles(_ context.Context, _ ScanRequest) (string, error) {
	return f.onvifMedia, nil
}
func (f fakeScannerClient) ReolinkDeviceInfo(_ context.Context, _ ScanRequest) (string, error) {
	return f.reolinkDev, nil
}
func (f fakeScannerClient) ReolinkChannelStatus(_ context.Context, _ ScanRequest) (string, error) {
	return f.reolinkChan, nil
}
func (f fakeScannerClient) ReolinkEncoding(_ context.Context, _ ScanRequest, channel int) (string, error) {
	return f.reolinkEnc[channel], nil
}

func TestScannerPrefersTapoONVIFWhenModelMatches(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(fakeScannerClient{
		ports: map[int]bool{2020: true, 554: true},
		onvifDevice: `<tds:GetDeviceInformationResponse><tds:Manufacturer>tp-link</tds:Manufacturer><tds:Model>Tapo C320WS</tds:Model></tds:GetDeviceInformationResponse>`,
		onvifMedia: `<trt:Profiles token="profile_1"><tt:Name>mainStream</tt:Name><tt:Encoding>H264</tt:Encoding><tt:Width>2560</tt:Width><tt:Height>1440</tt:Height></trt:Profiles><trt:Profiles token="profile_2"><tt:Name>minorStream</tt:Name><tt:Encoding>H264</tt:Encoding><tt:Width>640</tt:Width><tt:Height>360</tt:Height></trt:Profiles>`,
	})
	profile, err := scanner.Scan(context.Background(), ScanRequest{Host: "192.168.0.4", Username: "user", Password: "pass"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if profile.Adapter != "tplink-tapo-c320ws" {
		t.Fatalf("adapter = %q", profile.Adapter)
	}
}

func TestScannerUsesGenericFallbackWhenNoKnownAdapterMatches(t *testing.T) {
	t.Parallel()

	scanner := NewScanner(fakeScannerClient{ports: map[int]bool{554: true}})
	profile, err := scanner.Scan(context.Background(), ScanRequest{
		Name:   "manual",
		Host:   "10.0.0.50",
		RTSPURL: "rtsp://user:pass@10.0.0.50:554/stream",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if profile.Adapter != "generic-rtsp" {
		t.Fatalf("adapter = %q", profile.Adapter)
	}
}
```

- [ ] **Step 2: Run scanner tests and verify failure**

Run:

```bash
go test ./internal/cameraprofile -run Scanner -count=1
```

Expected: FAIL because scanner types do not exist.

- [ ] **Step 3: Implement scanner interfaces**

Create `internal/cameraprofile/scanner.go`:

```go
package cameraprofile

import (
	"context"
	"fmt"
)

type ScanRequest struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	RTSPPort  int    `json:"rtspPort,omitempty"`
	HTTPPort  int    `json:"httpPort,omitempty"`
	ONVIFPort int    `json:"onvifPort,omitempty"`
	Adapter   string `json:"adapter,omitempty"`
	RTSPURL   string `json:"rtspUrl,omitempty"`
}

type ScannerClient interface {
	PortOpen(ctx context.Context, host string, port int) bool
	ONVIFDeviceInfo(ctx context.Context, req ScanRequest) (string, error)
	ONVIFProfiles(ctx context.Context, req ScanRequest) (string, error)
	ReolinkDeviceInfo(ctx context.Context, req ScanRequest) (string, error)
	ReolinkChannelStatus(ctx context.Context, req ScanRequest) (string, error)
	ReolinkEncoding(ctx context.Context, req ScanRequest, channel int) (string, error)
}

type Scanner struct {
	client ScannerClient
}

func NewScanner(client ScannerClient) *Scanner {
	return &Scanner{client: client}
}

type fallbackNetworkScannerClient struct{}

func NewNetworkScannerClient() ScannerClient {
	return fallbackNetworkScannerClient{}
}

func (fallbackNetworkScannerClient) PortOpen(context.Context, string, int) bool { return false }
func (fallbackNetworkScannerClient) ONVIFDeviceInfo(context.Context, ScanRequest) (string, error) {
	return "", fmt.Errorf("network scanner client not configured")
}
func (fallbackNetworkScannerClient) ONVIFProfiles(context.Context, ScanRequest) (string, error) {
	return "", fmt.Errorf("network scanner client not configured")
}
func (fallbackNetworkScannerClient) ReolinkDeviceInfo(context.Context, ScanRequest) (string, error) {
	return "", fmt.Errorf("network scanner client not configured")
}
func (fallbackNetworkScannerClient) ReolinkChannelStatus(context.Context, ScanRequest) (string, error) {
	return "", fmt.Errorf("network scanner client not configured")
}
func (fallbackNetworkScannerClient) ReolinkEncoding(context.Context, ScanRequest, int) (string, error) {
	return "", fmt.Errorf("network scanner client not configured")
}

func (s *Scanner) Scan(ctx context.Context, req ScanRequest) (DeviceProfile, error) {
	if req.RTSPPort == 0 {
		req.RTSPPort = 554
	}
	if req.ONVIFPort == 0 {
		req.ONVIFPort = 2020
	}
	if req.Host == "" && req.RTSPURL == "" {
		return DeviceProfile{}, fmt.Errorf("host or rtspUrl is required")
	}
	if req.Adapter == "generic-rtsp" || req.RTSPURL != "" && req.Adapter == "" && !s.client.PortOpen(ctx, req.Host, req.ONVIFPort) {
		return GenericRTSPProfile(req.Name, req.Host, req.RTSPURL), nil
	}
	if s.client.PortOpen(ctx, req.Host, req.ONVIFPort) {
		device, devErr := s.client.ONVIFDeviceInfo(ctx, req)
		profiles, mediaErr := s.client.ONVIFProfiles(ctx, req)
		if devErr == nil && mediaErr == nil && device != "" && profiles != "" {
			onvifProfile, err := ParseTapoONVIF(device, profiles)
			if err == nil && onvifProfile.Model == "Tapo C320WS" {
				onvifProfile.Host = req.Host
				onvifProfile.RTSPPort = req.RTSPPort
				return onvifProfile, nil
			}
		}
	}
	if req.Adapter == "reolink-duo-wifi" || s.client.PortOpen(ctx, req.Host, 443) || s.client.PortOpen(ctx, req.Host, 80) {
		dev, devErr := s.client.ReolinkDeviceInfo(ctx, req)
		channels, channelErr := s.client.ReolinkChannelStatus(ctx, req)
		if devErr == nil && channelErr == nil && dev != "" && channels != "" {
			enc := map[int]string{}
			for channel := 0; channel < 4; channel++ {
				value, err := s.client.ReolinkEncoding(ctx, req, channel)
				if err == nil && value != "" {
					enc[channel] = value
				}
			}
			profile, err := ParseReolink(dev, channels, enc, req.Host, req.RTSPPort)
			if err == nil && profile.Model == "Reolink Duo WiFi" {
				return profile, nil
			}
		}
	}
	return GenericRTSPProfile(req.Name, req.Host, req.RTSPURL), nil
}
```

- [ ] **Step 4: Run scanner tests**

Run:

```bash
go test ./internal/cameraprofile -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cameraprofile/scanner.go internal/cameraprofile/scanner_test.go
git commit -m "Add read-only camera scanner core"
```

---

### Task 4: Generate go2rtc Role Streams

**Files:**
- Modify: `internal/stream/go2rtc.go`
- Modify: `internal/stream/go2rtc_test.go`

**Interfaces:**
- Consumes: Task 1 `store.Camera.Streams`, `CameraStream.Go2RTCStreamName`, `CameraStream.URL`.
- Produces:
  - `func (g *Go2RTC) WriteConfig(cameras []store.Camera) error` emits role streams when `camera.Streams` is non-empty.

- [ ] **Step 1: Write failing go2rtc config test**

Append to `internal/stream/go2rtc_test.go`:

```go
func TestWriteConfigUsesRoleStreamsWhenPresent(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "go2rtc.yaml")
	g := NewGo2RTC(path)
	cameras := []store.Camera{{
		Name:       "소방서3",
		StreamName: "3",
		URL:        "rtsp://user:pass@192.168.0.12:554/h264Preview_01_main",
		Streams: []store.CameraStream{
			{
				Role:             store.CameraStreamRoleRecording,
				URL:              "rtsp://user:pass@192.168.0.12:554/h264Preview_01_main",
				Go2RTCStreamName: "3-recording",
			},
			{
				Role:             store.CameraStreamRoleLive,
				URL:              "rtsp://user:pass@192.168.0.12:554/h264Preview_01_sub",
				Go2RTCStreamName: "3-live",
			},
		},
	}}

	if err := g.WriteConfig(cameras); err != nil {
		t.Fatalf("write config: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(raw)
	for _, want := range []string{"3-recording:", "3-live:", "h264Preview_01_main", "h264Preview_01_sub"} {
		if !strings.Contains(content, want) {
			t.Fatalf("config missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "\n  3:\n") {
		t.Fatalf("legacy stream should not be emitted when role streams exist:\n%s", content)
	}
}
```

Add imports:

```go
import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"camstation/internal/store"
)
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/stream -run TestWriteConfigUsesRoleStreamsWhenPresent -count=1
```

Expected: FAIL because `WriteConfig` still emits only `camera.StreamName`.

- [ ] **Step 3: Update `WriteConfig`**

In `internal/stream/go2rtc.go`, replace the camera stream loop with:

```go
for _, camera := range cameras {
	wroteRoleStream := false
	for _, cameraStream := range camera.Streams {
		if cameraStream.URL == "" || cameraStream.Go2RTCStreamName == "" {
			continue
		}
		buf.WriteString(fmt.Sprintf("  %s:\n", yamlKey(cameraStream.Go2RTCStreamName)))
		buf.WriteString(fmt.Sprintf("    - %s\n", quoteYAML(cameraStream.URL)))
		wroteRoleStream = true
	}
	if wroteRoleStream {
		continue
	}
	if camera.URL == "" || camera.StreamName == "" {
		continue
	}
	buf.WriteString(fmt.Sprintf("  %s:\n", yamlKey(camera.StreamName)))
	buf.WriteString(fmt.Sprintf("    - %s\n", quoteYAML(camera.URL)))
}
```

- [ ] **Step 4: Run stream tests**

Run:

```bash
go test ./internal/stream -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/stream/go2rtc.go internal/stream/go2rtc_test.go
git commit -m "Generate go2rtc role streams"
```

---

### Task 5: Recorder Uses Recording Role Streams

**Files:**
- Modify: `internal/recorder/recorder.go`
- Modify: `internal/recorder/recorder_test.go`

**Interfaces:**
- Consumes: Task 1 `Camera.StreamForRole(store.CameraStreamRoleRecording)`.
- Produces:
  - recorder `Status.Workers[].StreamName` equals recording role stream name for role-aware cameras.
  - archive paths still use `camera.Name`.

- [ ] **Step 1: Write failing recorder input selection test**

Append to `internal/recorder/recorder_test.go`:

```go
func TestRecordingStreamNameUsesRecordingRole(t *testing.T) {
	camera := store.Camera{
		Name:       "소방서3",
		StreamName: "3",
		Streams: []store.CameraStream{{
			Role:             store.CameraStreamRoleRecording,
			Go2RTCStreamName: "3-recording",
		}, {
			Role:             store.CameraStreamRoleLive,
			Go2RTCStreamName: "3-live",
		}},
	}
	streamName, input := recordingInputForCamera("rtsp://127.0.0.1:8554", camera)
	if streamName != "3-recording" {
		t.Fatalf("streamName = %q, want 3-recording", streamName)
	}
	if input != "rtsp://127.0.0.1:8554/3-recording" {
		t.Fatalf("input = %q", input)
	}
}
```

Add import:

```go
import "camstation/internal/store"
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/recorder -run TestRecordingStreamNameUsesRecordingRole -count=1
```

Expected: FAIL because helper does not exist.

- [ ] **Step 3: Add helper and use it**

In `internal/recorder/recorder.go`, add:

```go
func recordingInputForCamera(rtspBase string, camera store.Camera) (string, string) {
	streamName := camera.StreamName
	if stream, ok := camera.StreamForRole(store.CameraStreamRoleRecording); ok && stream.Go2RTCStreamName != "" {
		streamName = stream.Go2RTCStreamName
	}
	return streamName, fmt.Sprintf("%s/%s", rtspBase, streamName)
}
```

In `Manager.Start`, replace:

```go
input := fmt.Sprintf("%s/%s", m.rtspBase, camera.StreamName)
```

with:

```go
workerStreamName, input := recordingInputForCamera(m.rtspBase, camera)
camera.StreamName = workerStreamName
```

This keeps the worker's `camera.StreamName` aligned with the recording role
for segment metadata while preserving `camera.Name` for archive paths.

- [ ] **Step 4: Run recorder tests**

Run:

```bash
go test ./internal/recorder -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/recorder/recorder.go internal/recorder/recorder_test.go
git commit -m "Record from camera recording role streams"
```

---

### Task 6: Backend Scan And Save APIs

**Files:**
- Modify: `cmd/camstationd/main.go`
- Modify: `cmd/camstationd/main_test.go`
- Modify: `internal/store/store.go`

**Interfaces:**
- Consumes: Task 1 store role APIs, Task 3 scanner core.
- Produces:
  - `POST /api/cameras/scan`
  - `POST /api/cameras` accepts role-aware payload
  - `GET /api/cameras` returns `layoutKey`, `recordingStreamName`, `liveStreamName`, `streams`

- [ ] **Step 1: Write API route regression test**

Append to `cmd/camstationd/main_test.go`:

```go
func TestCameraAPISourceContainsProfileRouterEndpoints(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "cmd", "camstationd", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	content := string(source)
	for _, required := range []string{
		"POST /api/cameras/scan",
		"recordingStreamName",
		"liveStreamName",
		"ReplaceCameraStreams",
		"cameraprofile.NewScanner",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("camera API missing profile router requirement %q", required)
		}
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./cmd/camstationd -run TestCameraAPISourceContainsProfileRouterEndpoints -count=1
```

Expected: FAIL naming missing `/api/cameras/scan`.

- [ ] **Step 3: Add API request/response structs**

In `cmd/camstationd/main.go`, import the profile package:

```go
import "camstation/internal/cameraprofile"
```

Add local request structs near the camera routes:

```go
type cameraScanRequest struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	RTSPPort  int    `json:"rtspPort"`
	HTTPPort  int    `json:"httpPort"`
	ONVIFPort int    `json:"onvifPort"`
	Adapter   string `json:"adapter"`
	RTSPURL   string `json:"rtspUrl"`
}

type cameraCreateRequest struct {
	Name                string                       `json:"name"`
	Host                string                       `json:"host"`
	Username            string                       `json:"username"`
	Password            string                       `json:"password"`
	Adapter             string                       `json:"adapter"`
	Manufacturer        string                       `json:"manufacturer"`
	Model               string                       `json:"model"`
	RTSPPort            int                          `json:"rtspPort"`
	HTTPPort            int                          `json:"httpPort"`
	ONVIFPort           int                          `json:"onvifPort"`
	ChannelIndex        *int                         `json:"channelIndex"`
	LegacyURL           string                       `json:"url"`
	LegacyStreamName    string                       `json:"streamName"`
	RecordingStreamName string                       `json:"recordingStreamName"`
	LiveStreamName      string                       `json:"liveStreamName"`
	Streams             []cameraCreateStreamRequest  `json:"streams"`
	LastScan            map[string]any               `json:"lastScan"`
}

type cameraCreateStreamRequest struct {
	Role             store.CameraStreamRole `json:"role"`
	Label            string                 `json:"label"`
	Source           string                 `json:"source"`
	URL              string                 `json:"url"`
	Go2RTCStreamName string                 `json:"go2rtcStreamName"`
	Codec            string                 `json:"codec"`
	Width            int                    `json:"width"`
	Height           int                    `json:"height"`
	FPS              float64                `json:"fps"`
	BitrateKbps      int                    `json:"bitrateKbps"`
	ProfileToken     string                 `json:"profileToken"`
}
```

- [ ] **Step 4: Add `POST /api/cameras/scan` route**

Add route before `POST /api/cameras`:

```go
mux.HandleFunc("POST /api/cameras/scan", func(w http.ResponseWriter, r *http.Request) {
	var req cameraScanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	scanner := cameraprofile.NewScanner(cameraprofile.NewNetworkScannerClient())
	profile, err := scanner.Scan(r.Context(), cameraprofile.ScanRequest{
		Name:      req.Name,
		Host:      req.Host,
		Username:  req.Username,
		Password:  req.Password,
		RTSPPort:  req.RTSPPort,
		HTTPPort:  req.HTTPPort,
		ONVIFPort: req.ONVIFPort,
		Adapter:   req.Adapter,
		RTSPURL:   req.RTSPURL,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, redactDeviceProfile(profile))
})
```

Add the redaction helper used by the route:

```go
func redactDeviceProfile(profile cameraprofile.DeviceProfile) cameraprofile.DeviceProfile {
	for channelIndex := range profile.Channels {
		for candidateIndex := range profile.Channels[channelIndex].Candidates {
			candidate := &profile.Channels[channelIndex].Candidates[candidateIndex]
			if candidate.URL != "" {
				candidate.RedactedURL = store.RedactURL(candidate.URL)
				candidate.URL = ""
			}
		}
	}
	return profile
}
```

Add this wrapper in `internal/store/store.go` so scan redaction can reuse the
existing credential scrubber:

```go
func RedactURL(rawURL string) string {
	return redactCameraURL(rawURL)
}
```

- [ ] **Step 5: Update `POST /api/cameras` to store streams**

In the existing camera create route:

- Continue accepting `{name, url}` for legacy compatibility.
- If `req.Streams` is non-empty, save camera metadata and call
  `db.ReplaceCameraStreams`.
- Use `streamName(req.Name, 1)` as stable camera key unless
  `LegacyStreamName` is supplied.
- Set `LastScanJSON`.

Core save block:

```go
func primaryURL(req cameraCreateRequest) string {
	if len(req.Streams) > 0 {
		for _, stream := range req.Streams {
			if stream.Role == store.CameraStreamRoleRecording && stream.URL != "" {
				return stream.URL
			}
		}
	}
	return req.LegacyURL
}

func stableCameraKey(req cameraCreateRequest) string {
	if strings.TrimSpace(req.LegacyStreamName) != "" {
		return strings.TrimSpace(req.LegacyStreamName)
	}
	return streamName(req.Name, 1)
}

saved, err := db.UpsertCamera(r.Context(), store.Camera{
	Name:           req.Name,
	URL:            primaryURL(req),
	StreamName:     stableCameraKey(req),
	Manufacturer:   req.Manufacturer,
	Model:          req.Model,
	ProfileAdapter: req.Adapter,
	Host:           req.Host,
	RTSPPort:       req.RTSPPort,
	HTTPPort:       req.HTTPPort,
	ONVIFPort:      req.ONVIFPort,
	ChannelIndex:   req.ChannelIndex,
	State:          state,
	LastScanJSON:   req.LastScan,
	LastProbeJSON:  toMap(result),
})
```

Then:

```go
if len(req.Streams) > 0 {
	streams := make([]store.CameraStream, 0, len(req.Streams))
	for _, item := range req.Streams {
		streams = append(streams, store.CameraStream{
			CameraID:         saved.ID,
			Role:             item.Role,
			Label:            item.Label,
			Source:           item.Source,
			URL:              item.URL,
			Go2RTCStreamName: item.Go2RTCStreamName,
			Codec:            item.Codec,
			Width:            item.Width,
			Height:           item.Height,
			FPS:              item.FPS,
			BitrateKbps:      item.BitrateKbps,
			ProfileToken:     item.ProfileToken,
			State:            state,
		})
	}
	if err := db.ReplaceCameraStreams(r.Context(), saved.ID, streams); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
}
```

- [ ] **Step 6: Run backend route tests**

Run:

```bash
go test ./cmd/camstationd -run 'TestCameraAPI|TestRoutes' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/camstationd/main.go cmd/camstationd/main_test.go internal/store/store.go
git commit -m "Add camera scan and role save APIs"
```

---

### Task 7: Frontend API Types And Live Playback Mapping

**Files:**
- Modify: `web/src/app/api.ts`
- Modify: `web/src/app/queries.ts`
- Modify: `web/src/components/live/LiveWorkspace.tsx`
- Modify: `web/src/pages/ControlRoomPage.tsx`
- Modify: `cmd/camstationd/main_test.go`

**Interfaces:**
- Consumes: Task 6 API fields `layoutKey`, `recordingStreamName`, `liveStreamName`, `streams`.
- Produces:
  - frontend `CameraStream`, `CameraScanRequest`, `DeviceProfile`
  - `useScanCamera`
  - live grid uses `camera.layoutKey ?? camera.streamName` for layout and `camera.liveStreamName ?? camera.streamName` for playback.

- [ ] **Step 1: Write source regression test**

Append to `cmd/camstationd/main_test.go`:

```go
func TestLiveWorkspaceSeparatesLayoutKeyFromPlaybackStream(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "components", "live", "LiveWorkspace.tsx"))
	if err != nil {
		t.Fatalf("read LiveWorkspace: %v", err)
	}
	content := string(source)
	for _, required := range []string{
		"cameraKey(",
		"playbackStreamName(",
		"liveStreamName",
		"layoutKey",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("LiveWorkspace missing role-aware playback requirement %q", required)
		}
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./cmd/camstationd -run TestLiveWorkspaceSeparatesLayoutKeyFromPlaybackStream -count=1
```

Expected: FAIL because helpers are missing.

- [ ] **Step 3: Update frontend API types**

In `web/src/app/api.ts`, add:

```ts
export type CameraStream = {
  id: number;
  camera_id: number;
  role: "recording" | "live" | "snapshot" | string;
  label: string;
  source: string;
  redactedUrl: string;
  go2rtcStreamName: string;
  codec?: string;
  width?: number;
  height?: number;
  fps?: number;
  bitrateKbps?: number;
  profileToken?: string;
  state: string;
  lastProbe?: Record<string, unknown>;
};

export type DeviceProfile = {
  adapter: string;
  manufacturer: string;
  model: string;
  firmware?: string;
  host: string;
  rtspPort?: number;
  httpPort?: number;
  onvifPort?: number;
  channels: Array<{
    index: number;
    name: string;
    online: boolean;
    candidates: Array<{
      profileId: string;
      label: string;
      roleHint: "recording" | "live" | "snapshot" | string;
      source: string;
      redactedUrl?: string;
      url?: string;
      codec?: string;
      width?: number;
      height?: number;
      fps?: number;
      bitrateKbps?: number;
    }>;
  }>;
  warnings?: string[];
};

export type CameraScanRequest = {
  name?: string;
  host: string;
  username: string;
  password: string;
  rtspPort?: number;
  httpPort?: number;
  onvifPort?: number;
  adapter?: string;
  rtspUrl?: string;
};
```

Extend `Camera`:

```ts
layoutKey: string;
recordingStreamName?: string;
liveStreamName?: string;
manufacturer?: string;
model?: string;
profileAdapter?: string;
host?: string;
channelIndex?: number | null;
streams?: CameraStream[];
```

Add API method:

```ts
scanCamera: (request: CameraScanRequest) =>
  request<DeviceProfile>("/api/cameras/scan", {
    method: "POST",
    body: JSON.stringify(request),
  }),
```

- [ ] **Step 4: Add query hook**

In `web/src/app/queries.ts`:

```ts
import { api, type CameraScanRequest, type CreateCamera, type LayoutProfile } from "./api";

export function useScanCamera() {
  return useMutation({
    mutationFn: (request: CameraScanRequest) => api.scanCamera(request),
  });
}
```

- [ ] **Step 5: Update live workspace helpers**

In `web/src/components/live/LiveWorkspace.tsx`, add helpers:

```tsx
function cameraKey(camera: Camera) {
  return camera.layoutKey || camera.streamName;
}

function playbackStreamName(camera: Camera) {
  return camera.liveStreamName || camera.streamName;
}
```

Replace layout identity uses:

```tsx
camera.streamName
```

with:

```tsx
cameraKey(camera)
```

where the value is used for layout item IDs, selected camera identity, zoomed
camera identity, and saved layout merging.

Replace playback use:

```tsx
<LiveVideo streamName={camera.streamName} viewport={videoViewport} onViewportChange={onVideoViewportChange} />
```

with:

```tsx
<LiveVideo streamName={playbackStreamName(camera)} viewport={videoViewport} onViewportChange={onVideoViewportChange} />
```

Timeline queries should continue to use the camera key in v1:

```tsx
const selectedTimeline = useTimeline(selectedCamera ? cameraKey(selectedCamera) : "", today);
```

- [ ] **Step 6: Update control room preview**

In `web/src/pages/ControlRoomPage.tsx`, change:

```tsx
const { videoRef } = useMseStream(camera.streamName);
```

to:

```tsx
const { videoRef } = useMseStream(camera.liveStreamName || camera.streamName);
```

- [ ] **Step 7: Run source tests and build**

Run:

```bash
go test ./cmd/camstationd -run TestLiveWorkspaceSeparatesLayoutKeyFromPlaybackStream -count=1
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add web/src/app/api.ts web/src/app/queries.ts web/src/components/live/LiveWorkspace.tsx web/src/pages/ControlRoomPage.tsx cmd/camstationd/main_test.go cmd/camstationd/web
git commit -m "Use live role streams for playback"
```

---

### Task 8: Camera Registration Wizard And Profile Settings UI

**Files:**
- Modify: `web/src/pages/CamerasPage.tsx`
- Modify: `web/src/styles/index.css`
- Modify: `cmd/camstationd/main_test.go`
- Modify: `cmd/camstationd/web/*`

**Interfaces:**
- Consumes: Task 7 `useScanCamera`, `useCreateCamera`, `DeviceProfile`, role stream fields.
- Produces: `/cameras` staged UI: connection details, scan results, role assignment, verify/save, and per-camera profile settings panel.

- [ ] **Step 1: Write source regression test**

Append to `cmd/camstationd/main_test.go`:

```go
func TestCamerasPageExposesProfileWizard(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "pages", "CamerasPage.tsx"))
	if err != nil {
		t.Fatalf("read CamerasPage: %v", err)
	}
	content := string(source)
	for _, required := range []string{
		"useScanCamera",
		"카메라 스캔",
		"프로파일 설정",
		"녹화용 스트림",
		"라이브 스트림",
		"new-camera-wizard",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("CamerasPage missing wizard requirement %q", required)
		}
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./cmd/camstationd -run TestCamerasPageExposesProfileWizard -count=1
```

Expected: FAIL because wizard text/hooks are missing.

- [ ] **Step 3: Replace simple RTSP form with wizard state**

In `web/src/pages/CamerasPage.tsx`, add state:

```tsx
const scanCamera = useScanCamera();
const [step, setStep] = useState<"connection" | "results" | "roles">("connection");
const [connection, setConnection] = useState({
  name: "",
  host: "",
  username: "",
  password: "",
  adapter: "auto",
});
const [scanResult, setScanResult] = useState<DeviceProfile | null>(null);
const [selectedChannel, setSelectedChannel] = useState(0);
const [recordingProfileId, setRecordingProfileId] = useState("");
const [liveProfileId, setLiveProfileId] = useState("");
```

Add scan submit:

```tsx
async function onScan(event: FormEvent<HTMLFormElement>) {
  event.preventDefault();
  const result = await scanCamera.mutateAsync({
    name: connection.name,
    host: connection.host,
    username: connection.username,
    password: connection.password,
    adapter: connection.adapter === "auto" ? undefined : connection.adapter,
  });
  setScanResult(result);
  const channel = result.channels[0];
  setSelectedChannel(channel?.index ?? 0);
  setRecordingProfileId(channel?.candidates.find((item) => item.roleHint === "recording")?.profileId ?? "");
  setLiveProfileId(channel?.candidates.find((item) => item.roleHint === "live")?.profileId ?? "");
  setStep("results");
}
```

- [ ] **Step 4: Add role save logic**

Add helper to map selected candidates to create payload:

```tsx
function activeCandidates(profile: DeviceProfile, channelIndex: number) {
  return profile.channels.find((item) => item.index === channelIndex)?.candidates ?? profile.channels[0]?.candidates ?? [];
}

function selectedStreams(profile: DeviceProfile) {
  const channel = profile.channels.find((item) => item.index === selectedChannel) ?? profile.channels[0];
  const recording = channel.candidates.find((item) => item.profileId === recordingProfileId);
  const live = channel.candidates.find((item) => item.profileId === liveProfileId);
  const cameraKey = slugKey(connection.name || `${profile.model}-${selectedChannel}`);
  return [
    recording && {
      role: "recording",
      label: recording.label,
      source: recording.source,
      url: recording.url ?? "",
      go2rtcStreamName: `${cameraKey}-recording`,
      codec: recording.codec,
      width: recording.width,
      height: recording.height,
      fps: recording.fps,
      bitrateKbps: recording.bitrateKbps,
      profileToken: recording.profileId,
    },
    live && {
      role: "live",
      label: live.label,
      source: live.source,
      url: live.url ?? "",
      go2rtcStreamName: `${cameraKey}-live`,
      codec: live.codec,
      width: live.width,
      height: live.height,
      fps: live.fps,
      bitrateKbps: live.bitrateKbps,
      profileToken: live.profileId,
    },
  ].filter(Boolean);
}
```

Add `slugKey` local helper:

```tsx
function slugKey(value: string) {
  return value
    .trim()
    .replace(/[^a-zA-Z0-9가-힣_-]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "") || "camera";
}
```

Add save:

```tsx
async function onSaveProfile() {
  if (!scanResult) return;
  await createCamera.mutateAsync({
    name: connection.name || scanResult.model || "Camera",
    host: connection.host,
    username: connection.username,
    password: connection.password,
    adapter: scanResult.adapter,
    manufacturer: scanResult.manufacturer,
    model: scanResult.model,
    rtspPort: scanResult.rtspPort,
    httpPort: scanResult.httpPort,
    onvifPort: scanResult.onvifPort,
    channelIndex: selectedChannel,
    streams: selectedStreams(scanResult),
    lastScan: scanResult,
  });
  setStep("connection");
  setScanResult(null);
}
```

- [ ] **Step 5: Add wizard markup**

The first viewport of `/cameras` should show the working registration form, not
a marketing explanation. Use existing `Panel`, `Button`, and table styles. Add
visible Korean labels:

```tsx
<div className="new-camera-wizard">
  <div className="new-wizard-steps">
    <span className={step === "connection" ? "active" : ""}>접속 정보</span>
    <span className={step === "results" ? "active" : ""}>스캔 결과</span>
    <span className={step === "roles" ? "active" : ""}>역할 설정</span>
  </div>
  {step === "connection" && (
    <form className="space-y-3" onSubmit={onScan}>
      <input className="new-form-control" value={connection.host} onChange={(event) => setConnection({ ...connection, host: event.target.value })} placeholder="192.168.0.12" required />
      <input className="new-form-control" value={connection.username} onChange={(event) => setConnection({ ...connection, username: event.target.value })} placeholder="계정" required />
      <input className="new-form-control" value={connection.password} onChange={(event) => setConnection({ ...connection, password: event.target.value })} placeholder="비밀번호" type="password" required />
      <Button type="submit" variant="primary">카메라 스캔</Button>
    </form>
  )}
  {step === "results" && scanResult && (
    <div className="new-table-wrap">
      <table className="new-table">
        <tbody>
          <tr><td>모델</td><td>{scanResult.manufacturer} {scanResult.model}</td></tr>
          <tr><td>채널</td><td>{scanResult.channels.length}</td></tr>
        </tbody>
      </table>
      <Button type="button" onClick={() => setStep("roles")}>역할 설정</Button>
    </div>
  )}
  {step === "roles" && scanResult && (
    <div className="space-y-3">
      <label className="block space-y-2">
        <span>녹화용 스트림</span>
        <select className="new-form-control" value={recordingProfileId} onChange={(event) => setRecordingProfileId(event.target.value)}>
          {activeCandidates(scanResult, selectedChannel).map((item) => <option key={item.profileId} value={item.profileId}>{item.label}</option>)}
        </select>
      </label>
      <label className="block space-y-2">
        <span>라이브 스트림</span>
        <select className="new-form-control" value={liveProfileId} onChange={(event) => setLiveProfileId(event.target.value)}>
          {activeCandidates(scanResult, selectedChannel).map((item) => <option key={item.profileId} value={item.profileId}>{item.label}</option>)}
        </select>
      </label>
      <Button type="button" variant="primary" onClick={onSaveProfile}>저장 후 적용</Button>
    </div>
  )}
</div>
```

Ensure the page contains:

- `카메라 스캔`
- `녹화용 스트림`
- `라이브 스트림`
- `프로파일 설정`

- [ ] **Step 6: Add profile settings panel for existing cameras**

In each camera row, show manufacturer/model and streams:

```tsx
<button className="new-ghost" type="button" onClick={() => setProfileCamera(camera)}>
  프로파일 설정
</button>
```

Panel body:

```tsx
{profileCamera?.streams?.map((stream) => (
  <div className="new-profile-stream" key={`${stream.role}-${stream.go2rtcStreamName}`}>
    <strong>{stream.role === "recording" ? "녹화" : stream.role === "live" ? "라이브" : stream.role}</strong>
    <span>{stream.go2rtcStreamName}</span>
    <span>{stream.width && stream.height ? `${stream.width}x${stream.height}` : "-"}</span>
    <Badge value={stream.state} />
  </div>
))}
```

- [ ] **Step 7: Add CSS**

Append to `web/src/styles/index.css`:

```css
.new-camera-wizard {
  display: grid;
  gap: 14px;
}

.new-wizard-steps {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 12px;
  color: rgb(148 163 184);
}

.new-wizard-steps span {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 6px;
  padding: 6px 9px;
}

.new-wizard-steps .active {
  border-color: rgba(45, 212, 191, 0.55);
  color: rgb(204 251 241);
  background: rgba(20, 184, 166, 0.12);
}

.new-profile-stream {
  display: grid;
  grid-template-columns: minmax(70px, 0.8fr) minmax(140px, 1.5fr) minmax(90px, 1fr) auto;
  gap: 10px;
  align-items: center;
  border-top: 1px solid rgba(148, 163, 184, 0.12);
  padding: 10px 0;
  font-size: 12px;
}
```

- [ ] **Step 8: Run frontend build and source tests**

Run:

```bash
go test ./cmd/camstationd -run TestCamerasPageExposesProfileWizard -count=1
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add web/src/pages/CamerasPage.tsx web/src/styles/index.css cmd/camstationd/main_test.go cmd/camstationd/web
git commit -m "Add camera profile registration wizard"
```

---

### Task 9: Network Scanner Client For Current Cameras

**Files:**
- Modify: `internal/cameraprofile/scanner.go`
- Create: `internal/cameraprofile/network_client.go`
- Create: `internal/cameraprofile/network_client_test.go`

**Interfaces:**
- Consumes: Task 3 `ScannerClient`.
- Produces:
  - `func NewNetworkScannerClient() ScannerClient`
  - ONVIF WS-Security UsernameToken queries for Tapo.
  - Reolink login/query/logout API calls for Duo WiFi.

- [ ] **Step 1: Write request construction tests**

Create `internal/cameraprofile/network_client_test.go`:

```go
package cameraprofile

import (
	"strings"
	"testing"
)

func TestONVIFEnvelopeUsesUsernameTokenDigest(t *testing.T) {
	envelope, err := onvifEnvelope("user", "pass", "<tds:GetDeviceInformation/>")
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	for _, required := range []string{
		"UsernameToken",
		"<wsse:Username>user</wsse:Username>",
		"PasswordDigest",
		"Nonce",
		"Created",
		"<tds:GetDeviceInformation/>",
	} {
		if !strings.Contains(envelope, required) {
			t.Fatalf("envelope missing %q:\n%s", required, envelope)
		}
	}
	if strings.Contains(envelope, ">pass<") {
		t.Fatalf("plain password leaked in ONVIF envelope")
	}
}

func TestReolinkLoginBodyDoesNotLogPassword(t *testing.T) {
	body := reolinkLoginBody("user", "pass")
	if !strings.Contains(body, `"cmd":"Login"`) {
		t.Fatalf("login body missing command: %s", body)
	}
	if !strings.Contains(body, `"password":"pass"`) {
		t.Fatalf("login body must send password to device")
	}
	if redacted := redactSensitiveJSON(body); strings.Contains(redacted, "pass") {
		t.Fatalf("redacted body leaked password: %s", redacted)
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
go test ./internal/cameraprofile -run 'ONVIFEnvelope|ReolinkLoginBody' -count=1
```

Expected: FAIL because helpers do not exist.

- [ ] **Step 3: Implement network client helpers**

First remove the fallback-only `fallbackNetworkScannerClient` type and
`NewNetworkScannerClient` function from `internal/cameraprofile/scanner.go`.
Keep `ScanRequest`, `ScannerClient`, `Scanner`, `NewScanner`, and `Scan` in
`scanner.go`.

Then create `internal/cameraprofile/network_client.go` with:

```go
package cameraprofile

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type networkScannerClient struct {
	http *http.Client
}

func NewNetworkScannerClient() ScannerClient {
	return networkScannerClient{http: &http.Client{Timeout: 12 * time.Second}}
}

func (n networkScannerClient) PortOpen(ctx context.Context, host string, port int) bool {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
```

Implement ONVIF envelope:

```go
func onvifEnvelope(username, password, body string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	created := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	sum := sha1.Sum(append(append(nonce, []byte(created)...), []byte(password)...))
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:wsse="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd" xmlns:wsu="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
<s:Header><wsse:Security s:mustUnderstand="1"><wsse:UsernameToken><wsse:Username>%s</wsse:Username><wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">%s</wsse:Password><wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">%s</wsse:Nonce><wsu:Created>%s</wsu:Created></wsse:UsernameToken></wsse:Security></s:Header>
<s:Body>%s</s:Body></s:Envelope>`, username, base64.StdEncoding.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(nonce), created, body), nil
}
```

- [ ] **Step 4: Implement ONVIF calls**

Add:

```go
func (n networkScannerClient) ONVIFDeviceInfo(ctx context.Context, req ScanRequest) (string, error) {
	return n.onvifCall(ctx, req, `<tds:GetDeviceInformation/>`)
}

func (n networkScannerClient) ONVIFProfiles(ctx context.Context, req ScanRequest) (string, error) {
	return n.onvifCall(ctx, req, `<trt:GetProfiles/>`)
}

func (n networkScannerClient) onvifCall(ctx context.Context, req ScanRequest, body string) (string, error) {
	if req.ONVIFPort == 0 {
		req.ONVIFPort = 2020
	}
	envelope, err := onvifEnvelope(req.Username, req.Password, body)
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("http://%s:%d/onvif/service", req.Host, req.ONVIFPort)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(envelope))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", `application/soap+xml; charset=utf-8`)
	resp, err := n.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("onvif returned %s", resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
```

- [ ] **Step 5: Implement Reolink calls**

Add:

```go
func reolinkLoginBody(username, password string) string {
	raw, _ := json.Marshal([]map[string]any{{
		"cmd": "Login",
		"param": map[string]any{"User": map[string]any{
			"Version": "0", "userName": username, "password": password,
		}},
	}})
	return string(raw)
}

func redactSensitiveJSON(raw string) string {
	return regexp.MustCompile(`"password"\s*:\s*"[^"]*"`).ReplaceAllString(raw, `"password":"redacted"`)
}
```

Add:

```go
func (n networkScannerClient) ReolinkDeviceInfo(ctx context.Context, req ScanRequest) (string, error) {
	return n.reolinkCommand(ctx, req, "GetDevInfo", nil)
}

func (n networkScannerClient) ReolinkChannelStatus(ctx context.Context, req ScanRequest) (string, error) {
	return n.reolinkCommand(ctx, req, "GetChannelstatus", nil)
}

func (n networkScannerClient) ReolinkEncoding(ctx context.Context, req ScanRequest, channel int) (string, error) {
	return n.reolinkCommand(ctx, req, "GetEnc", map[string]any{"channel": channel})
}

func (n networkScannerClient) reolinkCommand(ctx context.Context, req ScanRequest, command string, param map[string]any) (string, error) {
	for _, scheme := range []string{"https", "http"} {
		token, err := n.reolinkLogin(ctx, scheme, req)
		if err != nil {
			continue
		}
		defer n.reolinkLogout(context.Background(), scheme, req.Host, token)
		body := []map[string]any{{"cmd": command}}
		if param != nil {
			body[0]["action"] = 1
			body[0]["param"] = param
		}
		encoded, _ := json.Marshal(body)
		url := fmt.Sprintf("%s://%s/api.cgi?cmd=%s&token=%s", scheme, req.Host, command, token)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
		if err != nil {
			return "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err := n.http.Do(httpReq)
		if err != nil {
			continue
		}
		raw, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return string(raw), nil
		}
	}
	return "", fmt.Errorf("reolink command %s failed", command)
}

func (n networkScannerClient) reolinkLogin(ctx context.Context, scheme string, req ScanRequest) (string, error) {
	url := fmt.Sprintf("%s://%s/api.cgi?cmd=Login", scheme, req.Host)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(reolinkLoginBody(req.Username, req.Password)))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var payload []struct {
		Value struct {
			Token struct {
				Name string `json:"name"`
			} `json:"Token"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	if len(payload) == 0 || payload[0].Value.Token.Name == "" {
		return "", fmt.Errorf("reolink login returned no token")
	}
	return payload[0].Value.Token.Name, nil
}

func (n networkScannerClient) reolinkLogout(ctx context.Context, scheme, host, token string) {
	body := `[{"cmd":"Logout"}]`
	url := fmt.Sprintf("%s://%s/api.cgi?cmd=Logout&token=%s", scheme, host, token)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := n.http.Do(httpReq)
	if err == nil {
		_ = resp.Body.Close()
	}
}
```

- [ ] **Step 6: Run profile package tests**

Run:

```bash
go test ./internal/cameraprofile -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cameraprofile/network_client.go internal/cameraprofile/network_client_test.go
git commit -m "Add network camera scanner client"
```

---

### Task 10: Migrate Current Cameras To Role Streams

**Files:**
- Modify: `internal/store/store.go`
- Create: `internal/store/migration_test.go`
- Modify: `cmd/camstationd/main.go`

**Interfaces:**
- Consumes: Task 1 stream storage, Task 2 known adapter policies.
- Produces:
  - deterministic startup migration/backfill for existing current cameras.

- [ ] **Step 1: Write migration test**

Create `internal/store/migration_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
)

func TestBackfillKnownCameraStreams(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := Open(filepath.Join(t.TempDir(), "camstation.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	camera, err := db.UpsertCamera(ctx, Camera{
		Name:       "소방서4",
		URL:        "rtsp://user:pass@192.168.0.12:554/h264Preview_02_main",
		StreamName: "4",
		State:      "streaming",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.BackfillKnownCameraStreams(ctx); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	streams, err := db.ListCameraStreams(ctx, camera.ID, true)
	if err != nil {
		t.Fatalf("streams: %v", err)
	}
	if len(streams) != 2 {
		t.Fatalf("stream count = %d, want 2", len(streams))
	}
	if streams[0].Go2RTCStreamName != "4-recording" {
		t.Fatalf("recording stream = %q", streams[0].Go2RTCStreamName)
	}
	if streams[1].Go2RTCStreamName != "4-live" {
		t.Fatalf("live stream = %q", streams[1].Go2RTCStreamName)
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./internal/store -run TestBackfillKnownCameraStreams -count=1
```

Expected: FAIL because `BackfillKnownCameraStreams` does not exist.

- [ ] **Step 3: Implement deterministic backfill**

Add to `internal/store/store.go`:

```go
func (d *DB) BackfillKnownCameraStreams(ctx context.Context) error {
	cameras, err := d.ListCameras(ctx, true)
	if err != nil {
		return err
	}
	for _, camera := range cameras {
		existing, err := d.ListCameraStreams(ctx, camera.ID, true)
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			continue
		}
		streams := knownStreamsForCamera(camera)
		if len(streams) == 0 {
			continue
		}
		if err := d.ReplaceCameraStreams(ctx, camera.ID, streams); err != nil {
			return err
		}
	}
	return nil
}
```

Add:

```go
func knownStreamsForCamera(camera Camera) []CameraStream {
	if camera.ID == 0 || camera.StreamName == "" || camera.URL == "" {
		return nil
	}
	recordingURL := camera.URL
	liveURL := camera.URL
	source := "legacy"
	switch {
	case strings.Contains(camera.URL, "/h264Preview_01_main"):
		liveURL = strings.Replace(camera.URL, "_main", "_sub", 1)
		source = "reolink-api"
	case strings.Contains(camera.URL, "/h264Preview_02_main"):
		liveURL = strings.Replace(camera.URL, "_main", "_sub", 1)
		source = "reolink-api"
	case strings.HasSuffix(camera.URL, "/stream1"):
		liveURL = strings.TrimSuffix(camera.URL, "/stream1") + "/stream2"
		source = "onvif"
	}
	return []CameraStream{
		{
			CameraID:         camera.ID,
			Role:             CameraStreamRoleRecording,
			Label:            "main",
			Source:           source,
			URL:              recordingURL,
			Go2RTCStreamName: camera.StreamName + "-recording",
			State:            camera.State,
		},
		{
			CameraID:         camera.ID,
			Role:             CameraStreamRoleLive,
			Label:            "sub",
			Source:           source,
			URL:              liveURL,
			Go2RTCStreamName: camera.StreamName + "-live",
			State:            camera.State,
		},
	}
}
```

- [ ] **Step 4: Call backfill during startup**

In `cmd/camstationd/main.go`, after `db.Migrate(ctx)` succeeds:

```go
if err := db.BackfillKnownCameraStreams(ctx); err != nil {
	log.Printf("camera stream backfill: %v", err)
}
```

- [ ] **Step 5: Run store tests**

Run:

```bash
go test ./internal/store -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/migration_test.go cmd/camstationd/main.go
git commit -m "Backfill current camera stream roles"
```

---

### Task 11: Timeline Translation And Status Surfaces

**Files:**
- Modify: `cmd/camstationd/main.go`
- Modify: `web/src/pages/RecordingsPage.tsx`
- Modify: `web/src/pages/StreamsPage.tsx`
- Modify: `cmd/camstationd/main_test.go`

**Interfaces:**
- Consumes: camera key vs role stream split.
- Produces: timeline queries by camera key map to recording stream; status pages show role streams.

- [ ] **Step 1: Write source guard**

Append to `cmd/camstationd/main_test.go`:

```go
func TestTimelineUsesRecordingRoleTranslation(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "cmd", "camstationd", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	content := string(source)
	for _, required := range []string{
		"recordingStreamForCameraKey",
		"ListRecordingSegments",
		"recordingStreamName",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("timeline missing recording role translation %q", required)
		}
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
go test ./cmd/camstationd -run TestTimelineUsesRecordingRoleTranslation -count=1
```

Expected: FAIL.

- [ ] **Step 3: Add timeline translation helper**

In `cmd/camstationd/main.go`:

```go
func recordingStreamForCameraKey(cameras []store.Camera, key string) string {
	for _, camera := range cameras {
		if camera.StreamName == key || camera.LayoutKey == key {
			if camera.RecordingStreamName != "" {
				return camera.RecordingStreamName
			}
			if stream, ok := camera.StreamForRole(store.CameraStreamRoleRecording); ok {
				return stream.Go2RTCStreamName
			}
			return camera.StreamName
		}
	}
	return key
}
```

In `/api/timeline`, before `ListRecordingSegments`, load cameras and translate:

```go
cameras, _ := db.ListCameras(r.Context(), true)
streamName = recordingStreamForCameraKey(cameras, streamName)
```

- [ ] **Step 4: Update status pages**

In `web/src/pages/RecordingsPage.tsx`, show camera name plus worker stream name:

```tsx
const cameraByRecordingStream = new Map(
  (cameras.data ?? []).map((camera) => [camera.recordingStreamName ?? camera.streamName, camera]),
);
```

In `web/src/pages/StreamsPage.tsx`, render camera stream roles if present:

```tsx
{camera.streams?.map((stream) => (
  <tr key={`${camera.id}-${stream.role}`}>
    <td>{camera.name}</td>
    <td>{stream.role}</td>
    <td>{stream.go2rtcStreamName}</td>
    <td>{stream.width && stream.height ? `${stream.width}x${stream.height}` : "-"}</td>
  </tr>
))}
```

- [ ] **Step 5: Run tests and build**

Run:

```bash
go test ./cmd/camstationd -run TestTimelineUsesRecordingRoleTranslation -count=1
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/camstationd/main.go cmd/camstationd/main_test.go web/src/pages/RecordingsPage.tsx web/src/pages/StreamsPage.tsx cmd/camstationd/web
git commit -m "Show camera role streams in status surfaces"
```

---

### Task 12: Full Verification And Runtime Apply

**Files:**
- Modify: `docs/07-implementation-status.md`

**Interfaces:**
- Consumes all previous tasks.
- Produces verified running CamStation using main recording streams and sub live streams.

- [ ] **Step 1: Run full test suite**

Run:

```bash
make test
cd web && npm run lint
cd web && npm run build
go test ./...
go build -o camstationd ./cmd/camstationd
```

Expected: all commands PASS.

- [ ] **Step 2: Restart development daemon safely**

Run:

```bash
scripts/camstationctl.sh restart
scripts/camstationctl.sh verify
```

Expected:

- `camstationd` running on `0.0.0.0:18080`
- go2rtc running with generated config
- recorder workers running when recording is enabled

- [ ] **Step 3: Verify generated go2rtc config**

Run:

```bash
sed -E 's#(rtsp://)[^:@/]+:[^@/]+@#\1redacted:redacted@#g' data/go2rtc.yaml
```

Expected entries include:

```text
3-recording
3-live
4-recording
4-live
1-recording
1-live
2-recording
2-live
camera-1-recording
camera-1-live
```

Reolink live entries must use `_sub`; Tapo live entries must use `stream2`.

- [ ] **Step 4: Verify API state**

Run:

```bash
curl -fsS http://127.0.0.1:18080/api/cameras | jq '.[] | {name, streamName, recordingStreamName, liveStreamName, streams: [.streams[]? | {role, go2rtcStreamName, redactedUrl, width, height}]}'
curl -fsS http://127.0.0.1:18080/api/streams/status | jq '.streams'
curl -fsS http://127.0.0.1:18080/api/recorders/status | jq '.workers'
```

Expected:

- each camera has one stable `streamName`
- each camera has `recordingStreamName`
- each camera has `liveStreamName`
- recorder worker inputs use `*-recording`
- no credentials appear

- [ ] **Step 5: Verify live and recording media**

Run:

```bash
timeout 12 ffprobe -v error -rtsp_transport tcp -select_streams v:0 \
  -show_entries stream=codec_name,width,height,r_frame_rate,avg_frame_rate \
  -of compact=p=0:nk=1 rtsp://127.0.0.1:8554/3-live

timeout 12 ffprobe -v error -rtsp_transport tcp -select_streams v:0 \
  -show_entries stream=codec_name,width,height,r_frame_rate,avg_frame_rate \
  -of compact=p=0:nk=1 rtsp://127.0.0.1:8554/3-recording
```

Expected:

- `3-live` is `640x360`
- `3-recording` is `2560x1440`

Run the same probe for the remaining live role streams:

```bash
for stream in 4-live 1-live 2-live camera-1-live; do
  timeout 12 ffprobe -v error -rtsp_transport tcp -select_streams v:0 \
    -show_entries stream=codec_name,width,height,r_frame_rate,avg_frame_rate \
    -of compact=p=0:nk=1 "rtsp://127.0.0.1:8554/${stream}"
done
```

- [ ] **Step 6: Browser smoke check**

Open:

```text
http://10.0.0.29:18080/live
http://10.0.0.29:18080/cameras
```

Expected:

- `/live` plays all cameras through live role streams.
- Browser video controls do not appear.
- Existing layout positions are preserved.
- `/cameras` shows the wizard and profile settings.
- Profile settings show model and recording/live role streams.

- [ ] **Step 7: Update implementation status**

In `docs/07-implementation-status.md`, add implemented bullets:

```markdown
- Camera profile router v1:
  - Tapo C320WS ONVIF profile detection
  - Reolink Duo WiFi API profile detection
  - camera registration scan workflow
  - recording/live stream role separation
  - go2rtc role stream generation
  - live uses sub streams while recording uses main streams
```

Add verified bullets with the exact runtime evidence from Steps 3-6.

- [ ] **Step 8: Commit**

```bash
git add docs/07-implementation-status.md
git commit -m "Document camera profile router verification"
```
