package vieweragent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	SchemaVersion  = 1
	machineDirName = "CamStation/Viewer"
)

type Config struct {
	SchemaVersion     int    `json:"schemaVersion"`
	ServerURL         string `json:"serverUrl"`
	DisplayName       string `json:"displayName"`
	InstallDir        string `json:"installDir"`
	ClientID          string `json:"clientId"`
	MonitoringUserSID string `json:"monitoringUserSid,omitempty"`
	AgentServiceSID   string `json:"agentServiceSid,omitempty"`
}

func ValidateServerURL(raw string) (string, error) {
	if raw == "" || raw != strings.TrimSpace(raw) || strings.ContainsAny(raw, "\r\n\t") {
		return "", errors.New("server URL is required without surrounding whitespace")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("server URL must be an absolute http or https URL")
	}
	if parsed.User != nil {
		return "", errors.New("server URL must not contain credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("server URL must not contain a path, query, or fragment")
	}
	parsed.Path = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func Configure(path, serverURL, displayName, installDir string) (Config, error) {
	validatedURL, err := ValidateServerURL(serverURL)
	if err != nil {
		return Config{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || strings.ContainsAny(displayName, "\r\n") {
		return Config{}, errors.New("display name is required")
	}
	installDir = filepath.Clean(strings.TrimSpace(installDir))
	if !filepath.IsAbs(installDir) {
		return Config{}, errors.New("install directory must be absolute")
	}

	clientID := ""
	monitoringUserSID := ""
	agentServiceSID := ""
	if current, loadErr := LoadConfig(path); loadErr == nil {
		clientID = current.ClientID
		monitoringUserSID = current.MonitoringUserSID
		agentServiceSID = current.AgentServiceSID
	} else if !errors.Is(loadErr, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load existing config: %w", loadErr)
	}
	if clientID == "" {
		clientID, err = newClientID()
		if err != nil {
			return Config{}, err
		}
	}
	config := Config{
		SchemaVersion:     SchemaVersion,
		ServerURL:         validatedURL,
		DisplayName:       displayName,
		InstallDir:        installDir,
		ClientID:          clientID,
		MonitoringUserSID: monitoringUserSID,
		AgentServiceSID:   agentServiceSID,
	}
	if err := atomicWriteJSON(path, config); err != nil {
		return Config{}, fmt.Errorf("save config: %w", err)
	}
	return config, nil
}

func LoadConfig(path string) (Config, error) {
	var config Config
	if err := readBoundedJSON(path, &config); err != nil {
		return Config{}, err
	}
	serverURL, err := ValidateServerURL(config.ServerURL)
	if err != nil {
		return Config{}, err
	}
	if config.SchemaVersion != SchemaVersion || config.ClientID == "" || strings.TrimSpace(config.DisplayName) == "" || !filepath.IsAbs(config.InstallDir) {
		return Config{}, errors.New("invalid viewer Agent config")
	}
	config.ServerURL = serverURL
	return config, nil
}

func DefaultMachineDir() string {
	if programData := os.Getenv("ProgramData"); programData != "" {
		return filepath.Join(programData, "CamStation", "Viewer")
	}
	return filepath.FromSlash("/var/lib/camstation/viewer")
}

func DefaultConfigPath() string { return filepath.Join(DefaultMachineDir(), "config.json") }

func newClientID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate client ID: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}
