package vieweragent

import (
	"path/filepath"
	"testing"
)

func TestValidateServerURLRejectsUnsafeAddresses(t *testing.T) {
	for _, raw := range []string{
		"", " file:///tmp/server", "file:///tmp/server", "javascript:alert(1)",
		"data:text/plain,no", "http://user:pass@camstation.local", "http://",
		"http://camstation.local/api", "http://camstation.local/?token=x",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ValidateServerURL(raw); err == nil {
				t.Fatalf("ValidateServerURL(%q) succeeded", raw)
			}
		})
	}
}

func TestConfigurePreservesStableClientID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	installDir := filepath.Join(t.TempDir(), "install")
	first, err := Configure(path, "http://camstation.local:18080", "Control Room", installDir)
	if err != nil {
		t.Fatal(err)
	}
	if first.ClientID == "" {
		t.Fatal("client ID was not generated")
	}
	second, err := Configure(path, "https://camstation.local", "Wall 2", installDir)
	if err != nil {
		t.Fatal(err)
	}
	if second.ClientID != first.ClientID {
		t.Fatalf("client ID changed: %q -> %q", first.ClientID, second.ClientID)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DisplayName != "Wall 2" || loaded.ServerURL != "https://camstation.local" {
		t.Fatalf("unexpected persisted config: %+v", loaded)
	}
}

func TestConfigurePreservesInstallerOwnedSIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	installDir := filepath.Join(t.TempDir(), "install")
	config, err := Configure(path, "http://camstation.local", "Wall", installDir)
	if err != nil {
		t.Fatal(err)
	}
	config.MonitoringUserSID = "S-1-5-21-1"
	config.AgentServiceSID = "S-1-5-80-2"
	if err := atomicWriteJSON(path, config); err != nil {
		t.Fatal(err)
	}
	updated, err := Configure(path, "http://camstation.local", "Wall 2", installDir)
	if err != nil {
		t.Fatal(err)
	}
	if updated.MonitoringUserSID != config.MonitoringUserSID || updated.AgentServiceSID != config.AgentServiceSID {
		t.Fatalf("installer-owned SIDs were lost: %+v", updated)
	}
}

func TestInstallerConfigurationPersistsUpdateTrustAndIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	installDir := filepath.Join(t.TempDir(), "install")
	config, err := ConfigureInstaller(path, InstallerConfig{
		ServerURL: "http://camstation.local:18080", DisplayName: "Wall A", InstallDir: installDir,
		MonitoringUserSID: "S-1-5-21-1", AgentServiceSID: "S-1-5-80-2", AllowDevelopmentUnsigned: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientID != config.ClientID || loaded.MonitoringUserSID != "S-1-5-21-1" || loaded.AgentServiceSID != "S-1-5-80-2" || !loaded.AllowDevelopmentUnsigned {
		t.Fatalf("installer config not persisted: %+v", loaded)
	}
}
