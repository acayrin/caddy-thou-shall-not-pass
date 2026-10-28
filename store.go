package tsnp

import (
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// failStore tracks wrong-answer attempts per client subnet (v4 /24, v6 /64)
// and blocks subnets that exceed the budget, mirroring Cerberus's
// max_pending semantics but counting explicit failures.
type failStore struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	blockTTL time.Duration
	fails    map[string]*failRec
	blocked  map[string]time.Time
}

type failRec struct {
	start time.Time
	n     int
}

func newFailStore(max int, window, blockTTL time.Duration) *failStore {
	return &failStore{
		max:      max,
		window:   window,
		blockTTL: blockTTL,
		fails:    map[string]*failRec{},
		blocked:  map[string]time.Time{},
	}
}

// Add records one failure for key; reports whether the key is now blocked.
func (s *failStore) Add(key string) (blocked bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if until, ok := s.blocked[key]; ok {
		if now.Before(until) {
			return true
		}
		delete(s.blocked, key)
	}
	rec := s.fails[key]
	if rec == nil || now.Sub(rec.start) > s.window {
		rec = &failRec{start: now}
		s.fails[key] = rec
	}
	rec.n++
	if rec.n >= s.max {
		s.blocked[key] = now.Add(s.blockTTL)
		delete(s.fails, key)
		return true
	}
	return false
}

func (s *failStore) Blocked(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.blocked[key]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(s.blocked, key)
		return false
	}
	return true
}

func (s *failStore) Clear(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fails, key)
}

// NetKey normalizes an IP string (with or without port) to its blocking key:
// IPv4 /24, IPv6 /64. Unparseable input is keyed as-is.
func NetKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		if a2, err2 := netip.ParseAddrPort(ip); err2 == nil {
			addr = a2.Addr()
		} else {
			return ip
		}
	}
	addr = addr.Unmap()
	if addr.Is4() {
		p := addr.As4()
		return fmt.Sprintf("%d.%d.%d.0", p[0], p[1], p[2])
	}
	if addr.Is6() {
		p := addr.As16()
		var masked [16]byte
		copy(masked[:], p[:8]) // /64
		return netip.AddrFrom16(masked).String()
	}
	return addr.String()
}
