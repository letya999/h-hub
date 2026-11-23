package svc

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// leases is the in-memory lease registry. Leases are intentionally
// not durable (brief): a restart invalidates every outstanding lease
// and materializations stay ephemeral. TTL is under two minutes.
const leaseTTL = 90 * time.Second

type leaseBook struct {
	mu   sync.Mutex
	byID map[string]lease
}

type lease struct {
	ref string
	sub string
	exp int64
}

func newLeaseBook() *leaseBook { return &leaseBook{byID: map[string]lease{}} }

func (b *leaseBook) issue(ref, sub string, now time.Time) (id string, exp int64) {
	id = "lease-" + randHex(8)
	exp = now.Add(leaseTTL).Unix()
	b.mu.Lock()
	b.byID[id] = lease{ref: ref, sub: sub, exp: exp}
	b.mu.Unlock()
	return id, exp
}

// take removes the lease; expiry and unknown ids look the same to the
// caller — the value must already be treated as gone.
func (b *leaseBook) take(id string, now time.Time) (lease, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, ok := b.byID[id]
	if !ok || l.exp <= now.Unix() {
		delete(b.byID, id)
		return lease{}, false
	}
	delete(b.byID, id)
	return l, true
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b[:]); err != nil {
		panic("rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}
