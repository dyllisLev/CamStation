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
	SchemaVersion            int    `json:"schemaVersion"`
	ServerURL                string `json:"serverUrl"`
	DisplayName              string `json:"displayName"`
	InstallDir               string `json:"installDir"`
	ClientID                 string `json:"clientId"`
	MonitoringUserSID        string `json:"monitoringUserSid,omitempty"`
	AgentServiceSID          string `json:"agentServiceSid,omitempty"`
	AllowDevelopmentUnsigned bool   `json:"allowDevelopmentUnsigned,omitempty"`
	SignerThumbprint         string `json:"signerThumbprint,omitempty"`
}

type InstallerConfig struct {
	ServerURL                string
	DisplayName              string
	InstallDir               string
	MonitoringUserSID        string
	AgentServiceSID          string
	AllowDevelopmentUnsigned bool
	SignerThumbprint         string
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
	allowDevelopmentUnsigned := false
	signerThumbprint := ""
	if current, loadErr := LoadConfig(path); loadErr == nil {
		clientID = current.ClientID
		monitoringUserSID = current.MonitoringUserSID
		agentServiceSID = current.AgentServiceSID
		allowDevelopmentUnsigned = current.AllowDevelopmentUnsigned
		signerThumbprint = current.SignerThumbprint
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
		SchemaVersion:            SchemaVersion,
		ServerURL:                validatedURL,
		DisplayName:              displayName,
		InstallDir:               installDir,
		ClientID:                 clientID,
		MonitoringUserSID:        monitoringUserSID,
		AgentServiceSID:          agentServiceSID,
		AllowDevelopmentUnsigned: allowDevelopmentUnsigned,
		SignerThumbprint:         signerThumbprint,
	}
	if err := atomicWriteJSON(path, config); err != nil {
		return Config{}, fmt.Errorf("save config: %w", err)
	}
	return config, nil
}

func ConfigureInstaller(path string, installer InstallerConfig) (Config, error) {
	config, err := Configure(path, installer.ServerURL, installer.DisplayName, installer.InstallDir)
	if err != nil {
		return Config{}, err
	}
	if !validInstallerSID(installer.MonitoringUserSID) || !validInstallerSID(installer.AgentServiceSID) {
		return Config{}, errors.New("valid monitoring and Agent service SIDs are required")
	}
	if installer.SignerThumbprint != "" && !validThumbprint(installer.SignerThumbprint) {
		return Config{}, errors.New("invalid signer thumbprint")
	}
	config.MonitoringUserSID = installer.MonitoringUserSID
	config.AgentServiceSID = installer.AgentServiceSID
	config.AllowDevelopmentUnsigned = installer.AllowDevelopmentUnsigned
	config.SignerThumbprint = strings.ToLower(installer.SignerThumbprint)
	if err := atomicWriteJSON(path, config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func validInstallerSID(value string) bool {
	if !strings.HasPrefix(value, "S-") || len(value) > 184 || strings.ContainsAny(value, "\r\n\t ") {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "S-") {
		if (char < '0' || char > '9') && char != '-' {
			return false
		}
	}
	return true
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
	if (config.MonitoringUserSID != "" && !validInstallerSID(config.MonitoringUserSID)) ||
		(config.AgentServiceSID != "" && !validInstallerSID(config.AgentServiceSID)) ||
		(config.SignerThumbprint != "" && !validThumbprint(config.SignerThumbprint)) ||
		(config.AllowDevelopmentUnsigned && config.SignerThumbprint != "") {
		return Config{}, errors.New("invalid installer-owned Agent config")
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
