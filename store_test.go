package tsnp

import (
	"testing"
	"time"
)

func TestFailStoreBlocks(t *testing.T) {
	s := newFailStore(3, time.Hour, time.Hour)
	if s.Blocked("k") {
		t.Fatal("blocked before any fails")
	}
	if b := s.Add("k"); b {
		t.Fatal("blocked at 1 fail")
	}
	s.Add("k")
	if b := s.Add("k"); !b {
		t.Fatal("not blocked at max_fails")
	}
	if !s.Blocked("k") {
		t.Fatal("Blocked false after block")
	}
	// third fail after block stays blocked without counting
	if b := s.Add("k"); !b {
		t.Fatal("Add after block returned false")
	}
}

func TestFailStoreWindowReset(t *testing.T) {
	s := newFailStore(3, time.Millisecond, time.Hour)
	s.Add("k")
	s.Add("k")
	time.Sleep(5 * time.Millisecond)
	if b := s.Add("k"); b {
		t.Fatal("window did not reset")
	}
}

func TestFailStoreClear(t *testing.T) {
	s := newFailStore(3, time.Hour, time.Hour)
	s.Add("k")
	s.Add("k")
	s.Clear("k")
	if b := s.Add("k"); b {
		t.Fatal("clear did not reset counter")
	}
}

func TestNetKey(t *testing.T) {
	cases := map[string]string{
		"203.0.113.66":            "203.0.113.0",
		"127.0.0.1":               "127.0.0.0",
		"[2001:db8:1234:5678::9]": "2001:db8:1234:5678::",
		"garbage":                 "garbage",
	}
	for in, want := range cases {
		in2 := in
		if len(in2) > 0 && in2[0] == '[' {
			// bracketed form: NetKey receives bare IPs usually; strip
			in2 = in2[1 : len(in2)-1]
		}
		if got := NetKey(in2); got != want {
			t.Errorf("NetKey(%q) = %q, want %q", in2, got, want)
		}
	}
	if got := NetKey("203.0.113.66:443"); got != "203.0.113.0" {
		t.Errorf("NetKey with port = %q", got)
	}
}
