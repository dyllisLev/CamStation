//go:build windows

package viewerinstall

import (
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc/mgr"
)

func TestWindowsRecoveryAdapterUsesSCMNoAction(t *testing.T) {
	want := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 120 * time.Second},
		{Type: mgr.NoAction, Delay: 0},
	}
	got, err := windowsRecoveryActions()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("actions=%+v err=%v", got, err)
	}
}
