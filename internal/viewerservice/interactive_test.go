package viewerservice

import "testing"

func TestInteractivePeerAcceptsAnActiveUserSession(t *testing.T) {
	tests := []struct {
		name             string
		sessionID        uint32
		tokenInteractive bool
		sessionActive    bool
		want             bool
	}{
		{name: "interactive token", sessionID: 1, tokenInteractive: true, want: true},
		{name: "active scheduled task session", sessionID: 1, sessionActive: true, want: true},
		{name: "session zero", sessionID: 0, tokenInteractive: true, sessionActive: true, want: false},
		{name: "inactive noninteractive token", sessionID: 3, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isInteractivePeer(test.sessionID, test.tokenInteractive, test.sessionActive); got != test.want {
				t.Fatalf("isInteractivePeer(%d, %t, %t)=%t, want %t", test.sessionID, test.tokenInteractive, test.sessionActive, got, test.want)
			}
		})
	}
}
