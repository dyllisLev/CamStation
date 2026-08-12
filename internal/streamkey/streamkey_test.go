package streamkey

import (
	"strings"
	"testing"
)

func TestValidAcceptsProductionAndGeneratedKeys(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"집-마당",
		"집-창고1",
		"집-창고2",
		"소방서1",
		"소방서2",
		"소방서3",
		"소방서4",
		"염소장",
		"소방서5",
		"goat-yard",
		"camera_01",
		strings.Repeat("a", MaxBytes),
	} {
		if !Valid(value) {
			t.Errorf("Valid(%q) = false, want true", value)
		}
	}
}

func TestValidRejectsUnsafeOrAmbiguousKeys(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"-camera",
		"camera-",
		"camera/name",
		`camera\name`,
		"camera.name",
		"camera name",
		"camera?viewer=1",
		"camera#fragment",
		"camera%2Fname",
		"rtsp:camera",
		"camera\nname",
		"camera\tname",
		"camera\x00name",
		"camera📷",
		strings.Repeat("a", MaxBytes+1),
		string([]byte{0xff, 0xfe}),
	} {
		if Valid(value) {
			t.Errorf("Valid(%q) = true, want false", value)
		}
	}
}
