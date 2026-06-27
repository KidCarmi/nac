package server

import (
	"testing"
	"time"
)

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	now := time.Now()
	rl := newRateLimiter(3, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Fatalf("4th request should be blocked")
	}
	// A different client is independent.
	if !rl.allow("5.6.7.8") {
		t.Fatalf("different client should be allowed")
	}
}

func TestRateLimiter_WindowResets(t *testing.T) {
	now := time.Now()
	clock := now
	rl := newRateLimiter(1, func() time.Time { return clock })
	if !rl.allow("1.2.3.4") {
		t.Fatalf("first allowed")
	}
	if rl.allow("1.2.3.4") {
		t.Fatalf("second blocked in same window")
	}
	clock = now.Add(time.Minute + time.Second)
	if !rl.allow("1.2.3.4") {
		t.Fatalf("should be allowed after window reset")
	}
}

func TestStatusClass(t *testing.T) {
	cases := map[int]string{200: "2xx", 201: "2xx", 302: "3xx", 404: "4xx", 500: "5xx", 100: "1xx"}
	for code, want := range cases {
		if got := statusClass(code); got != want {
			t.Errorf("statusClass(%d)=%s want %s", code, got, want)
		}
	}
}
