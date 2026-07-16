//go:build windows

package vieweragent

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

func verifyAuthenticode(path, thumbprint string, allowUnsigned bool) error {
	if allowUnsigned {
		return nil
	}
	script := `$s=Get-AuthenticodeSignature -LiteralPath $args[0]; if($s.Status -ne 'Valid'){exit 2}; if($s.SignerCertificate.Thumbprint -ne $args[1]){exit 3}`
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script, path, strings.ToUpper(thumbprint))
	if err := command.Run(); err != nil {
		return errors.New("Authenticode verification failed")
	}
	return nil
}

func launchUpdateDetached(path string, args []string) error {
	command := exec.Command(path, args...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
