package vieweragent

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"

	"camstation/internal/viewerinstall"
)

func installedReleaseIdentity(installDir string) (string, string) {
	current, err := LoadCurrentRelease(installDir)
	if err != nil {
		return "", ""
	}
	return current.Version, current.Digest
}

func (agent *Agent) acceptHeartbeatCommit(response HeartbeatResponse) error {
	if response.CommitToken == "" {
		return nil
	}
	desired := response.DesiredRelease
	if desired == nil || len(response.CommitToken) != 64 || strings.ToLower(response.CommitToken) != response.CommitToken {
		return errors.New("invalid server update commit response")
	}
	if _, err := hex.DecodeString(response.CommitToken); err != nil {
		return errors.New("invalid server update commit token")
	}
	command := desired.Command()
	if err := validateCommand(command); err != nil {
		return err
	}
	journal, err := LoadUpdateJournal(agent.Paths.Update)
	if err != nil {
		return err
	}
	version, digest := installedReleaseIdentity(agent.Config.InstallDir)
	exact := journal.State == "installer_launched" && journal.CommandID == command.ID &&
		journal.PayloadHash == command.PayloadHash && journal.TargetVersion == command.DesiredVersion &&
		strings.EqualFold(journal.ArtifactSHA256, command.ArtifactSHA256) && journal.Generation == command.Generation &&
		journal.TransactionID != "" && version == command.DesiredVersion && strings.EqualFold(digest, command.ArtifactSHA256)
	if !exact {
		return errors.New("server update commit response does not match installed transaction")
	}
	layout := viewerinstall.Layout{InstallDir: agent.Config.InstallDir, StateDir: filepath.Dir(agent.Paths.Update)}
	return viewerinstall.SaveCommitMarker(layout, viewerinstall.CommitMarker{
		TransactionID: journal.TransactionID, CommandID: command.ID, PayloadHash: command.PayloadHash,
		Version: command.DesiredVersion, Digest: strings.ToLower(command.ArtifactSHA256),
		Generation: command.Generation, Token: response.CommitToken,
	})
}
