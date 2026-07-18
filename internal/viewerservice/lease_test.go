package viewerservice

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLeaseIsOwnedByConnectionAndVerifiedPeer(t *testing.T) {
	clock := newFakeClock(time.Unix(100, 0))
	leases := NewLeaseManager(clock.Now, 15*time.Second)
	peer := Peer{PID: 10, SessionID: 2, Interactive: true}
	first, err := leases.Acquire("connection-a", peer)
	if err != nil || first.ID == "" || first.PID != peer.PID || first.SessionID != peer.SessionID {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	if _, err := leases.Acquire("connection-b", Peer{PID: 11, SessionID: 3, Interactive: true}); !errors.Is(err, ErrLeaseBusy) {
		t.Fatalf("second acquire err=%v", err)
	}
	if err := leases.Refresh("connection-b", first.ID, peer); !errors.Is(err, ErrLeaseOwner) {
		t.Fatalf("foreign connection refresh err=%v", err)
	}
	if err := leases.Refresh("connection-a", first.ID, Peer{PID: 11, SessionID: 2, Interactive: true}); !errors.Is(err, ErrLeaseOwner) {
		t.Fatalf("foreign PID refresh err=%v", err)
	}
	if err := leases.Refresh("connection-a", first.ID, Peer{PID: 10, SessionID: 3, Interactive: true}); !errors.Is(err, ErrLeaseOwner) {
		t.Fatalf("foreign session refresh err=%v", err)
	}
	if err := leases.Refresh("connection-a", "wrong-lease", peer); !errors.Is(err, ErrLeaseOwner) {
		t.Fatalf("wrong ID refresh err=%v", err)
	}
	if _, err := leases.Acquire("connection-c", Peer{PID: 12, SessionID: 4}); !errors.Is(err, ErrPeerIdentity) {
		t.Fatalf("noninteractive acquire err=%v", err)
	}
}

func TestLeaseWithOwnerRevalidatesOwnershipBeforeWrite(t *testing.T) {
	now := time.Now()
	manager := NewLeaseManager(func() time.Time { return now }, time.Second)
	peer := Peer{PID: 7, SessionID: 1, Interactive: true}
	lease, err := manager.Acquire("connection-a", peer)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	if err := manager.WithOwner("connection-b", func() error {
		called = true
		return nil
	}); !errors.Is(err, ErrLeaseOwner) || called {
		t.Fatalf("foreign err=%v called=%v", err, called)
	}
	if err := manager.WithOwner("connection-a", func() error {
		called = true
		return nil
	}); err != nil || !called {
		t.Fatalf("owner err=%v called=%v lease=%+v", err, called, lease)
	}
}

func TestLeaseRefreshesAndExpiresAfterFifteenSeconds(t *testing.T) {
	clock := newFakeClock(time.Unix(100, 0))
	leases := NewLeaseManager(clock.Now, 15*time.Second)
	peer := Peer{PID: 10, SessionID: 2, Interactive: true}
	lease, err := leases.Acquire("connection-a", peer)
	if err != nil {
		t.Fatal(err)
	}

	clock.Advance(14 * time.Second)
	if err := leases.Refresh("connection-a", lease.ID, peer); err != nil {
		t.Fatal(err)
	}
	clock.Advance(14 * time.Second)
	if leases.Available() {
		t.Fatal("refreshed lease expired early")
	}
	clock.Advance(time.Second)
	if !leases.Available() {
		t.Fatal("unrefreshed lease did not expire after 15 seconds")
	}
	if _, err := leases.Acquire("connection-b", Peer{PID: 11, SessionID: 3, Interactive: true}); err != nil {
		t.Fatalf("acquire after expiry: %v", err)
	}
}

func TestLeaseConnectionReleaseIsImmediate(t *testing.T) {
	leases := NewLeaseManager(time.Now, 15*time.Second)
	if _, err := leases.Acquire("connection-a", Peer{PID: 10, SessionID: 2, Interactive: true}); err != nil {
		t.Fatal(err)
	}
	leases.ReleaseConnection("connection-a")
	if !leases.Available() {
		t.Fatal("lease remains busy after connection release")
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock { return &fakeClock{now: now} }

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}
