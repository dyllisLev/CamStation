//go:build windows

package viewerservice

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

const viewerLogAppendMask = 0x00100004 // FILE_APPEND_DATA | SYNCHRONIZE

func secureViewerLogFile(path, userSID string) error {
	sid, err := windows.StringToSid(userSID)
	if err != nil {
		return fmt.Errorf("parse user SID: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("create viewer log: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close viewer log: %w", err)
	}
	sddl := fmt.Sprintf("D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;0x%08x;;;%s)", viewerLogAppendMask, sid.String())
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build viewer log DACL: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read viewer log DACL: %w", err)
	}
	information := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, information, nil, nil, dacl, nil); err != nil {
		return fmt.Errorf("apply viewer log DACL: %w", err)
	}
	return nil
}
