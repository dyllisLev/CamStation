package viewerservice

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var (
	ErrLeaseBusy    = errors.New("viewer lease is busy")
	ErrLeaseOwner   = errors.New("viewer lease owner does not match")
	ErrPeerIdentity = errors.New("verified peer identity is required")
)

type Peer struct {
	PID         uint32
	SessionID   uint32
	Interactive bool
	UserSID     string
}

type Lease struct {
	ID        string
	PID       uint32
	SessionID uint32
	ExpiresAt time.Time
}

type LeaseToken struct {
	ConnectionID string
	LeaseID      string
}

type LeaseManager struct {
	mu           sync.Mutex
	now          func() time.Time
	ttl          time.Duration
	lease        *Lease
	connectionID string
}

func NewLeaseManager(now func() time.Time, ttl time.Duration) *LeaseManager {
	if now == nil {
		now = time.Now
	}
	return &LeaseManager{now: now, ttl: ttl}
}

func (manager *LeaseManager) Acquire(connectionID string, peer Peer) (Lease, error) {
	if !validLeasePeer(connectionID, peer) {
		return Lease{}, ErrPeerIdentity
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked()
	if manager.lease != nil {
		return Lease{}, ErrLeaseBusy
	}
	id, err := newLeaseID()
	if err != nil {
		return Lease{}, err
	}
	lease := Lease{ID: id, PID: peer.PID, SessionID: peer.SessionID, ExpiresAt: manager.now().Add(manager.ttl)}
	manager.lease = &lease
	manager.connectionID = connectionID
	return lease, nil
}

func (manager *LeaseManager) Refresh(connectionID, leaseID string, peer Peer) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !manager.ownsLocked(connectionID, leaseID, peer) {
		return ErrLeaseOwner
	}
	manager.lease.ExpiresAt = manager.now().Add(manager.ttl)
	return nil
}

func (manager *LeaseManager) Authorize(connectionID, leaseID string, peer Peer) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !manager.ownsLocked(connectionID, leaseID, peer) {
		return ErrLeaseOwner
	}
	return nil
}

func (manager *LeaseManager) Release(connectionID, leaseID string, peer Peer) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if !manager.ownsLocked(connectionID, leaseID, peer) {
		return ErrLeaseOwner
	}
	manager.lease = nil
	manager.connectionID = ""
	return nil
}

func (manager *LeaseManager) ReleaseConnection(connectionID string) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.lease == nil || connectionID == "" || manager.connectionID != connectionID {
		return false
	}
	manager.lease = nil
	manager.connectionID = ""
	return true
}

func (manager *LeaseManager) Available() bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked()
	return manager.lease == nil
}

func (manager *LeaseManager) Owner() string {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked()
	return manager.connectionID
}

func (manager *LeaseManager) Token() (LeaseToken, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked()
	if manager.lease == nil || manager.connectionID == "" {
		return LeaseToken{}, false
	}
	return LeaseToken{ConnectionID: manager.connectionID, LeaseID: manager.lease.ID}, true
}

func (manager *LeaseManager) ValidateToken(token LeaseToken) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked()
	return manager.ownsTokenLocked(token)
}

func (manager *LeaseManager) WithToken(token LeaseToken, callback func() error) error {
	if callback == nil {
		return ErrLeaseOwner
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked()
	if !manager.ownsTokenLocked(token) {
		return ErrLeaseOwner
	}
	return callback()
}

// WithOwner runs callback while the current lease owner is held. This closes
// the check-then-write race used by service command delivery.
func (manager *LeaseManager) WithOwner(connectionID string, callback func() error) error {
	if callback == nil {
		return ErrLeaseOwner
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.expireLocked()
	if manager.lease == nil || connectionID == "" || manager.connectionID != connectionID {
		return ErrLeaseOwner
	}
	return callback()
}

func (manager *LeaseManager) ownsLocked(connectionID, leaseID string, peer Peer) bool {
	manager.expireLocked()
	return manager.lease != nil && validLeasePeer(connectionID, peer) && manager.connectionID == connectionID &&
		manager.lease.ID == leaseID && manager.lease.PID == peer.PID && manager.lease.SessionID == peer.SessionID
}

func (manager *LeaseManager) ownsTokenLocked(token LeaseToken) bool {
	return manager.lease != nil && token.ConnectionID != "" && token.LeaseID != "" &&
		manager.connectionID == token.ConnectionID && manager.lease.ID == token.LeaseID
}

func (manager *LeaseManager) expireLocked() {
	if manager.lease != nil && !manager.now().Before(manager.lease.ExpiresAt) {
		manager.lease = nil
		manager.connectionID = ""
	}
}

func validLeasePeer(connectionID string, peer Peer) bool {
	return connectionID != "" && peer.PID != 0 && peer.Interactive
}

func newLeaseID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
