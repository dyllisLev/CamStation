package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"camstation/internal/store"
)

func validateRequest(request StartRequest, target string) (string, string, error) {
	source := strings.TrimSpace(request.Source)
	if source == "" {
		return "", "", fmt.Errorf("backup source is required: %w", store.ErrValidation)
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return "", "", fmt.Errorf("backup source is unavailable: %w", store.ErrValidation)
	}
	target, err = cleanTarget(target)
	if err != nil {
		return "", "", err
	}
	prefix, err := cleanPrefix(request.Prefix)
	if err != nil {
		return "", "", err
	}
	if prefix == "" {
		return source, target, nil
	}
	return source, target + "/" + prefix, nil
}

func rcloneCopyArgs(source string, destination string) []string {
	return []string{"copy", source, destination, "--stats", "0", "--retries", "1"}
}

func cleanTarget(target string) (string, error) {
	target = strings.TrimRight(strings.TrimSpace(target), "/")
	if target == "" || strings.Contains(target, "\\") || strings.Contains(target, "..") || filepath.IsAbs(target) || strings.HasPrefix(target, "-") || !strings.Contains(target, ":") {
		return "", fmt.Errorf("backup target is invalid: %w", store.ErrValidation)
	}
	return target, nil
}

func cleanPrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "", nil
	}
	if strings.Contains(prefix, "\\") || strings.Contains(prefix, "..") || filepath.IsAbs(prefix) {
		return "", fmt.Errorf("backup prefix is invalid: %w", store.ErrValidation)
	}
	return prefix, nil
}
