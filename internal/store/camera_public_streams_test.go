package store

import "testing"

func TestRegisteredPublicStreamPreservesEffectiveCameraNames(t *testing.T) {
	db := openMigratedStore(t)
	camera, err := db.UpsertCamera(t.Context(), Camera{Name: "yard", URL: "rtsp://camera/private-input", StreamName: "yard", State: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.db.ExecContext(t.Context(), query, args...); err != nil {
			t.Fatal(err)
		}
	}
	check := func(name string, want bool) {
		t.Helper()
		got, err := db.IsRegisteredPublicStream(t.Context(), name)
		if err != nil || got != want {
			t.Fatalf("registered(%q)=%v, err=%v; want %v", name, got, err, want)
		}
	}
	check("yard", true)
	for _, output := range camera.Outputs {
		check(output.StreamName, true)
	}
	for _, name := range []string{"", "unknown", "rtsp://camera/private-input", " yard "} {
		check(name, false)
	}
	// Configured outputs supersede stale legacy columns, including on-demand
	// outputs; registration must not depend on runtime activation or readiness.
	exec(`UPDATE cameras SET recording_stream_name='stale-record',live_stream_name='stale-live' WHERE id=?`, camera.ID)
	exec(`UPDATE camera_outputs SET stream_name='custom-'||purpose,activation='on_demand' WHERE camera_id=?`, camera.ID)
	for _, name := range []string{"yard", "custom-recording", "custom-live", "custom-focus"} {
		check(name, true)
	}
	check("stale-record", false)
	check("stale-live", false)
	for _, output := range camera.Outputs {
		if output.StreamName != "yard" {
			check(output.StreamName, false)
		}
	}
	exec(`UPDATE camera_outputs SET stream_name='renamed-live' WHERE camera_id=? AND purpose='live'`, camera.ID)
	check("custom-live", false)
	check("renamed-live", true)
	exec(`UPDATE cameras SET enabled=0 WHERE id=?`, camera.ID)
	check("yard", false)
	check("renamed-live", false)
	check("custom-recording", false)
	check("custom-focus", false)
	exec(`UPDATE cameras SET enabled=1 WHERE id=?`, camera.ID)
	// Legacy names reappear only when no effective output overrides the role.
	exec(`DELETE FROM camera_outputs WHERE camera_id=?`, camera.ID)
	check("stale-record", true)
	check("stale-live", true)
	check("renamed-live", false)
	exec(`UPDATE cameras SET recording_stream_name='',live_stream_name='' WHERE id=?`, camera.ID)
	check("yard", true)
	check("stale-record", false)
	check("stale-live", false)
	if _, err := db.DeleteCamera(t.Context(), "yard"); err != nil {
		t.Fatal(err)
	}
	check("yard", false)
}
