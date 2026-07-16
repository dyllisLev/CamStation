//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"camstation/internal/viewerinstall"
	"golang.org/x/sys/windows"
)

func ensureElevated(args []string) (bool, error) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return false, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(executable)
	quoted := make([]string, len(args))
	for index, arg := range args {
		quoted[index] = syscall.EscapeArg(arg)
	}
	parameters, _ := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err := windows.ShellExecute(0, verb, file, parameters, nil, windows.SW_SHOWNORMAL); err != nil {
		return false, err
	}
	return true, nil
}

func waitParent(pid int, maximum time.Duration) error {
	if pid <= 0 {
		return nil
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, uint32(maximum/time.Millisecond))
	if err != nil {
		return err
	}
	if result == uint32(windows.WAIT_TIMEOUT) {
		return errors.New("Agent did not stop before update deadline")
	}
	return nil
}

func detachOwnedInstaller(layout viewerinstall.Layout, options installerOptions, args []string) (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	if options.detachedParentPID > 0 {
		if !detachedInstallerPath(executable) {
			return false, errors.New("detached installer handoff did not target a temporary helper")
		}
		return false, nil
	}
	if !needsDetachedInstaller(executable, layout, 0, options.mode) {
		return false, nil
	}
	dir, err := os.MkdirTemp("", "camstation-viewer-detached-*")
	if err != nil {
		return false, err
	}
	target := filepath.Join(dir, "CamStationViewerSetup.exe")
	if err := copyDetachedInstaller(executable, target); err != nil {
		return false, errors.Join(err, os.RemoveAll(dir))
	}
	childArgs := append(append([]string(nil), args...), "--detached-parent-pid", strconv.Itoa(os.Getpid()))
	command := exec.Command(target, childArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return false, errors.Join(err, os.RemoveAll(dir))
	}
	return true, command.Process.Release()
}

func cleanupDetachedInstaller() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if !detachedInstallerPath(executable) {
		return errors.New("refusing to clean a non-temporary detached installer")
	}
	dir := filepath.Dir(executable)
	executableName, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	dirName, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	return errors.Join(
		windows.MoveFileEx(executableName, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT),
		windows.MoveFileEx(dirName, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT),
	)
}

func detachedInstallerPath(executable string) bool {
	dir := filepath.Dir(filepath.Clean(executable))
	return strings.EqualFold(filepath.Dir(dir), filepath.Clean(os.TempDir())) && strings.HasPrefix(strings.ToLower(filepath.Base(dir)), "camstation-viewer-detached-")
}

func copyDetachedInstaller(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func removeInstallation(layout viewerinstall.Layout) error {
	var paths []string
	for _, root := range []string{layout.InstallDir, layout.StateDir} {
		_ = filepath.Walk(root, func(path string, _ os.FileInfo, _ error) error {
			paths = append(paths, path)
			return nil
		})
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	var result error
	for _, path := range paths {
		if err := os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) {
			continue
		}
		name, convertErr := windows.UTF16PtrFromString(path)
		if convertErr != nil {
			result = errors.Join(result, convertErr)
			continue
		}
		if err := windows.MoveFileEx(name, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}
