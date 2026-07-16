//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
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
