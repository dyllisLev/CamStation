package viewerservice

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildConfigNormalizesAndDefaultsAutoStart(t *testing.T) {
	got, err := BuildConfig(ConfigDraft{
		ServerURL: "https://cam.example/", DisplayName: "  관제실  ", AutoStart: true,
	}, MachineConfig{}, func() (string, error) { return "client-1", nil })
	if err != nil || got.ServerURL != "https://cam.example" || got.DisplayName != "관제실" ||
		got.ClientID != "client-1" || !got.AutoStart || got.SchemaVersion != ConfigSchemaVersion {
		t.Fatalf("config=%+v err=%v", got, err)
	}
}

func TestBuildConfigPreservesClientID(t *testing.T) {
	current := MachineConfig{SchemaVersion: 1, ServerURL: "https://old.example", DisplayName: "old", ClientID: "stable", AutoStart: true}
	got, err := BuildConfig(ConfigDraft{ServerURL: "https://new.example", DisplayName: "new"}, current,
		func() (string, error) { t.Fatal("generated a replacement ID"); return "", nil })
	if err != nil || got.ClientID != "stable" {
		t.Fatalf("config=%+v err=%v", got, err)
	}
}

func TestBuildConfigRejectsUnsafeServerURLs(t *testing.T) {
	for _, serverURL := range []string{
		"https://user:secret@cam.example",
		"https://cam.example/api",
		"https://cam.example?token=secret",
		"https://cam.example#status",
	} {
		t.Run(serverURL, func(t *testing.T) {
			_, err := BuildConfig(ConfigDraft{ServerURL: serverURL, DisplayName: "wall"}, MachineConfig{}, func() (string, error) {
				return "client-1", nil
			})
			if ErrorCode(err) != CodeInvalidInput {
				t.Fatalf("BuildConfig(%q) error=%v code=%q", serverURL, err, ErrorCode(err))
			}
		})
	}
}

func TestBuildConfigRejectsInvalidDisplayNames(t *testing.T) {
	for _, displayName := range []string{"  ", "wall\nname", "wall\x00name", "wall\u0085name"} {
		t.Run(displayName, func(t *testing.T) {
			_, err := BuildConfig(ConfigDraft{ServerURL: "https://cam.example", DisplayName: displayName}, MachineConfig{}, func() (string, error) {
				return "client-1", nil
			})
			if ErrorCode(err) != CodeInvalidInput {
				t.Fatalf("BuildConfig(%q) error=%v code=%q", displayName, err, ErrorCode(err))
			}
		})
	}
}

func TestCommitDoesNotOverwriteWorkingConfigWhenValidationFails(t *testing.T) {
	old := MachineConfig{SchemaVersion: ConfigSchemaVersion, ServerURL: "https://old.example", DisplayName: "old", ClientID: "stable", AutoStart: true}
	store := &memoryConfigStore{config: old}
	manager := ConfigManager{
		Store: store,
		Validator: validatorFunc(func(context.Context, ConfigDraft, string) error {
			return ErrRegistrationRejected
		}),
		NewID: func() (string, error) { t.Fatal("generated a replacement ID"); return "", nil },
	}

	_, err := manager.Commit(context.Background(), ConfigDraft{ServerURL: "https://new.example", DisplayName: "new"})
	if !errors.Is(err, ErrRegistrationRejected) {
		t.Fatalf("Commit error=%v", err)
	}
	if store.saves != 0 {
		t.Fatalf("Save called %d times", store.saves)
	}
	got, loadErr := store.Load(context.Background())
	if loadErr != nil || got != old {
		t.Fatalf("stored config=%+v err=%v", got, loadErr)
	}
}

func TestCommitMapsFailuresToStableCodes(t *testing.T) {
	old := MachineConfig{SchemaVersion: ConfigSchemaVersion, ServerURL: "https://old.example", DisplayName: "old", ClientID: "stable", AutoStart: true}
	tests := []struct {
		name      string
		draft     ConfigDraft
		store     *memoryConfigStore
		validator error
		want      string
	}{
		{name: "invalid input", draft: ConfigDraft{ServerURL: "file:///server", DisplayName: "wall"}, store: &memoryConfigStore{config: old}, want: CodeInvalidInput},
		{name: "server unreachable", draft: ConfigDraft{ServerURL: "https://cam.example", DisplayName: "wall"}, store: &memoryConfigStore{config: old}, validator: ErrServerUnreachable, want: CodeServerUnreachable},
		{name: "API incompatible", draft: ConfigDraft{ServerURL: "https://cam.example", DisplayName: "wall"}, store: &memoryConfigStore{config: old}, validator: ErrAPIIncompatible, want: CodeAPIIncompatible},
		{name: "registration rejected", draft: ConfigDraft{ServerURL: "https://cam.example", DisplayName: "wall"}, store: &memoryConfigStore{config: old}, validator: ErrRegistrationRejected, want: CodeRegistrationRejected},
		{name: "load failed", draft: ConfigDraft{ServerURL: "https://cam.example", DisplayName: "wall"}, store: &memoryConfigStore{loadErr: errors.New("registry unavailable")}, want: CodeStorageFailed},
		{name: "save failed", draft: ConfigDraft{ServerURL: "https://cam.example", DisplayName: "wall"}, store: &memoryConfigStore{config: old, saveErr: errors.New("registry denied")}, want: CodeStorageFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := ConfigManager{
				Store:     test.store,
				Validator: validatorFunc(func(context.Context, ConfigDraft, string) error { return test.validator }),
				NewID:     func() (string, error) { return "client-1", nil },
			}
			_, err := manager.Commit(context.Background(), test.draft)
			if got := ErrorCode(err); got != test.want {
				t.Fatalf("Commit error=%v code=%q, want %q", err, got, test.want)
			}
		})
	}
}

func TestRegistryDocumentRoundTrip(t *testing.T) {
	want := MachineConfig{SchemaVersion: ConfigSchemaVersion, ServerURL: "https://cam.example", DisplayName: "관제실", ClientID: "client-1", AutoStart: true}
	document, err := encodeRegistryDocument(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRegistryDocument(document)
	if err != nil || got != want {
		t.Fatalf("config=%+v err=%v", got, err)
	}
}

func TestRegistryDocumentRejectsUnknownFieldsAndExtraJSON(t *testing.T) {
	for _, document := range []string{
		`{"schemaVersion":1,"serverUrl":"https://cam.example","displayName":"wall","clientId":"client-1","autoStart":true,"secret":"no"}`,
		`{"schemaVersion":1,"serverUrl":"https://cam.example","displayName":"wall","clientId":"client-1","autoStart":true} {}`,
	} {
		if _, err := decodeRegistryDocument(document); !errors.Is(err, ErrConfigDecode) {
			t.Fatalf("decodeRegistryDocument error=%v", err)
		}
	}
}

func TestRegistryDocumentRejectsUnsupportedSchema(t *testing.T) {
	document := `{"schemaVersion":2,"serverUrl":"https://cam.example","displayName":"wall","clientId":"client-1","autoStart":true}`
	if _, err := decodeRegistryDocument(document); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("decodeRegistryDocument error=%v", err)
	}
}

func TestRegistryDocumentRejectsMissingClientID(t *testing.T) {
	document := `{"schemaVersion":1,"serverUrl":"https://cam.example","displayName":"wall","clientId":"","autoStart":true}`
	if _, err := decodeRegistryDocument(document); !errors.Is(err, ErrInvalidPersistedConfig) {
		t.Fatalf("decodeRegistryDocument error=%v", err)
	}
}

func TestRegistryDocumentRejectsValuesLargerThan64KiB(t *testing.T) {
	document := strings.Repeat("x", maxRegistryDocumentBytes+1)
	if _, err := decodeRegistryDocument(document); !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("decodeRegistryDocument error=%v", err)
	}
}

type memoryConfigStore struct {
	config  MachineConfig
	loadErr error
	saveErr error
	saves   int
}

func (store *memoryConfigStore) Load(context.Context) (MachineConfig, error) {
	if store.loadErr != nil {
		return MachineConfig{}, store.loadErr
	}
	return store.config, nil
}

func (store *memoryConfigStore) Save(_ context.Context, config MachineConfig) error {
	store.saves++
	if store.saveErr != nil {
		return store.saveErr
	}
	store.config = config
	return nil
}

type validatorFunc func(context.Context, ConfigDraft, string) error

func (validate validatorFunc) Validate(ctx context.Context, draft ConfigDraft, clientID string) error {
	return validate(ctx, draft, clientID)
}
