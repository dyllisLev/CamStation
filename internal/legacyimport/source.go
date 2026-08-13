package legacyimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"camstation/internal/store"
	"camstation/internal/streamkey"

	_ "modernc.org/sqlite"
)

var requiredLegacyColumns = map[string][]string{
	"cameras": {
		"id", "display_name", "location", "enabled", "archived_at", "main_stream_url",
		"sub_stream_url", "onvif_host", "onvif_port", "onvif_username", "onvif_password",
		"sort_order", "notes", "created_at", "updated_at",
	},
	"layouts": {
		"id", "name", "data", "timeline_collapsed", "grid_cols", "grid_rows", "created_at", "updated_at",
	},
	"settings": {"key", "value"},
}

var cameraLayoutLegacyColumns = map[string][]string{
	"cameras": {
		"id", "display_name", "enabled", "archived_at", "main_stream_url", "sub_stream_url", "sort_order",
	},
	"layouts": {
		"id", "name", "data", "timeline_collapsed", "grid_cols", "grid_rows",
	},
}

type legacyCamera struct {
	id            string
	displayName   string
	location      sql.NullString
	enabled       bool
	archived      bool
	mainURL       string
	subURL        sql.NullString
	onvifHost     sql.NullString
	onvifPort     sql.NullInt64
	onvifUsername sql.NullString
	onvifPassword sql.NullString
	sortOrder     int
	notes         sql.NullString
}

type legacyLayout struct {
	id                string
	name              string
	data              string
	timelineCollapsed bool
	gridCols          int
	gridRows          sql.NullInt64
}

func openLegacyReadOnly(ctx context.Context, path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve legacy snapshot path: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("SQLite source must be an existing regular, non-symlink file")
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open legacy snapshot: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open legacy snapshot read-only: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enforce legacy snapshot read-only mode: %w", err)
	}
	return db, nil
}

func inspectLegacySchema(ctx context.Context, db *sql.DB, manifest *Manifest) bool {
	return inspectLegacySchemaForScope(ctx, db, manifest, ScopeFull)
}

func inspectLegacySchemaForScope(ctx context.Context, db *sql.DB, manifest *Manifest, scope ImportScope) bool {
	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "SOURCE_QUICK_CHECK_FAILED", Message: "1.x snapshot quick-check could not be completed"})
		return false
	}
	manifest.QuickCheck = result
	if result != "ok" {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "SOURCE_QUICK_CHECK_FAILED", Message: "1.x snapshot failed SQLite quick-check"})
	}

	compatible := true
	requiredTables := requiredLegacyColumns
	if scope == ScopeCameraLayout {
		requiredTables = cameraLayoutLegacyColumns
	}
	tables := make([]string, 0, len(requiredTables))
	for table := range requiredTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		columns, err := tableColumns(ctx, db, table)
		if err != nil {
			compatible = false
			manifest.Blockers = append(manifest.Blockers, Finding{Code: "SOURCE_SCHEMA_UNREADABLE", Message: "required 1.x schema could not be inspected"})
			continue
		}
		missing := make([]string, 0)
		for _, required := range requiredTables[table] {
			if !columns[required] {
				missing = append(missing, required)
			}
		}
		if len(missing) > 0 {
			compatible = false
			manifest.Blockers = append(manifest.Blockers, Finding{
				Code:    "SOURCE_SCHEMA_MISMATCH",
				Message: fmt.Sprintf("required 1.x table %s is missing %d column(s)", table, len(missing)),
			})
		}
	}
	manifest.SchemaCompatible = compatible
	return compatible && result == "ok"
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func readLegacyCameras(ctx context.Context, db *sql.DB) ([]legacyCamera, int, error) {
	return readLegacyCamerasForScope(ctx, db, ScopeFull)
}

func readLegacyCamerasForScope(ctx context.Context, db *sql.DB, scope ImportScope) ([]legacyCamera, int, error) {
	if scope == ScopeCameraLayout {
		return readCameraLayoutCameras(ctx, db)
	}
	rows, err := db.QueryContext(ctx, `SELECT id,display_name,location,enabled,archived_at,main_stream_url,
		sub_stream_url,onvif_host,onvif_port,onvif_username,onvif_password,sort_order,notes
		FROM cameras ORDER BY sort_order,id`)
	if err != nil {
		return nil, 0, fmt.Errorf("read legacy cameras: %w", err)
	}
	defer rows.Close()
	var cameras []legacyCamera
	archivedCount := 0
	for rows.Next() {
		var row legacyCamera
		var enabled int
		var archivedAt sql.NullFloat64
		if err := rows.Scan(&row.id, &row.displayName, &row.location, &enabled, &archivedAt, &row.mainURL,
			&row.subURL, &row.onvifHost, &row.onvifPort, &row.onvifUsername, &row.onvifPassword,
			&row.sortOrder, &row.notes); err != nil {
			return nil, 0, fmt.Errorf("decode legacy camera row: %w", err)
		}
		row.enabled = enabled != 0
		row.archived = archivedAt.Valid
		if row.archived {
			archivedCount++
			continue
		}
		cameras = append(cameras, row)
	}
	return cameras, archivedCount, rows.Err()
}

func readCameraLayoutCameras(ctx context.Context, db *sql.DB) ([]legacyCamera, int, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,display_name,enabled,archived_at,main_stream_url,sub_stream_url,sort_order
		FROM cameras ORDER BY sort_order,id`)
	if err != nil {
		return nil, 0, fmt.Errorf("read legacy camera graph: %w", err)
	}
	defer rows.Close()
	var cameras []legacyCamera
	archivedCount := 0
	for rows.Next() {
		var row legacyCamera
		var enabled int
		var archivedAt sql.NullFloat64
		if err := rows.Scan(&row.id, &row.displayName, &enabled, &archivedAt, &row.mainURL, &row.subURL, &row.sortOrder); err != nil {
			return nil, 0, fmt.Errorf("decode legacy camera graph row: %w", err)
		}
		row.enabled = enabled != 0
		row.archived = archivedAt.Valid
		if row.archived {
			archivedCount++
			continue
		}
		cameras = append(cameras, row)
	}
	return cameras, archivedCount, rows.Err()
}

func readLegacyLayouts(ctx context.Context, db *sql.DB) ([]legacyLayout, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,name,data,timeline_collapsed,grid_cols,grid_rows FROM layouts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy layouts: %w", err)
	}
	defer rows.Close()
	var layouts []legacyLayout
	for rows.Next() {
		var row legacyLayout
		var collapsed int
		if err := rows.Scan(&row.id, &row.name, &row.data, &collapsed, &row.gridCols, &row.gridRows); err != nil {
			return nil, fmt.Errorf("decode legacy layout row: %w", err)
		}
		row.timelineCollapsed = collapsed != 0
		layouts = append(layouts, row)
	}
	return layouts, rows.Err()
}

func readLegacySettings(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key,value FROM settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("read legacy settings: %w", err)
	}
	defer rows.Close()
	settings := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("decode legacy settings row: %w", err)
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

func parseLegacyInt(settings map[string]string, key string, fallback int, manifest *Manifest) int {
	raw, ok := settings[key]
	if !ok || strings.TrimSpace(raw) == "" {
		manifest.Warnings = append(manifest.Warnings, Finding{Code: "SETTING_DEFAULTED", Message: key + " was absent and received the documented default"})
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "SETTING_INVALID", Message: key + " is not an integer"})
		return fallback
	}
	return value
}

func parseLegacyFloat(settings map[string]string, key string, fallback float64, manifest *Manifest) float64 {
	raw, ok := settings[key]
	if !ok || strings.TrimSpace(raw) == "" {
		manifest.Warnings = append(manifest.Warnings, Finding{Code: "SETTING_DEFAULTED", Message: key + " was absent and received the documented default"})
		return fallback
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "SETTING_INVALID", Message: key + " is not numeric"})
		return fallback
	}
	return value
}

func parseLegacyBool(settings map[string]string, key string, fallback bool, manifest *Manifest) bool {
	raw, ok := settings[key]
	if !ok || strings.TrimSpace(raw) == "" {
		manifest.Warnings = append(manifest.Warnings, Finding{Code: "SETTING_DEFAULTED", Message: key + " was absent and received the documented default"})
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		manifest.Blockers = append(manifest.Blockers, Finding{Code: "SETTING_INVALID", Message: key + " is not a boolean"})
		return fallback
	}
}

func decodeLayoutItems(raw string) ([]store.LayoutItem, error) {
	var items []store.LayoutItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	return items, nil
}

func validLegacyCameraKey(key string) bool {
	return streamkey.Valid(key)
}
