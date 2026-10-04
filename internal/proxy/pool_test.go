package proxy

import (
	"context"
	"testing"
	"time"
)

func quiet(string, ...any) {}

func TestPoolWithDirectSeedsLiveWithoutProbing(t *testing.T) {
	p := NewPool(5, 10, false, true, quiet)
	s := p.Stats()
	if !s.Direct {
		t.Error("Stats.Direct = false, want true when -use-direct is on")
	}
	// The local IP needs no validation, so it must be usable immediately
	// rather than after the first probe round.
	if s.Alive != 1 || s.Target != 5 {
		t.Errorf("alive = %d, target = %d; want 1 alive immediately", s.Alive, s.Target)
	}
}

func TestPoolWithoutDirectHasNoLocalIP(t *testing.T) {
	p := NewPool(5, 10, false, false, quiet)
	if p.Stats().Direct {
		t.Error("Stats.Direct = true, want false when -use-direct is off")
	}
	if p.Stats().Alive != 0 {
		t.Errorf("alive = %d, want 0 before any proxy is probed", p.Stats().Alive)
	}
}

// The local IP is only ever a filler. If it won ties it would take most
// requests, and its rate-limit budget is the one that cannot be replaced.
func TestNextPrefersProxyOverDirect(t *testing.T) {
	p := NewPool(5, 10, false, true, quiet)
	real := newProxy("1.2.3.4:8080")
	p.live = append(p.live, real)

	got := p.Next(context.Background(), time.Millisecond)
	if got != real {
		t.Fatalf("Next returned %q, want the proxy %q", got.Addr(), real.Addr())
	}
}

func TestNextFallsBackToDirectWhenNoProxyReady(t *testing.T) {
	p := NewPool(5, 10, false, true, quiet)
	// A proxy exists but is still paced, so direct is the only option.
	paced := newProxy("1.2.3.4:8080")
	paced.nextAt = time.Now().Add(time.Hour)
	p.live = append(p.live, paced)

	got := p.Next(context.Background(), time.Millisecond)
	if got == nil {
		t.Fatal("Next returned nil, want the direct client")
	}
	if !got.IsDirect() {
		t.Errorf("Next returned %q, want direct", got.Addr())
	}
}

func TestNextWithoutPoolReturnsNil(t *testing.T) {
	p := NewPool(5, 10, false, false, quiet)
	if got := p.Next(context.Background(), time.Millisecond); got != nil {
		t.Errorf("Next = %v, want nil so callers fall back to a direct client", got)
	}
}

// Retiring the local IP would remove the only source guaranteed to exist.
func TestReportFailureDoesNotRetireDirect(t *testing.T) {
	p := NewPool(5, 10, false, true, quiet)
	before := p.Stats().Alive

	p.ReportFailure(p.live[0]) // the direct member
	after := p.Stats()

	if after.Alive != before {
		t.Errorf("alive = %d, want %d: the local IP must survive a failure", after.Alive, before)
	}
	if after.Failures != 1 {
		t.Errorf("Failures = %d, want the failure still counted", after.Failures)
	}
}

func TestReportFailureRetiresProxy(t *testing.T) {
	p := NewPool(5, 10, false, true, quiet)
	pr := newProxy("1.2.3.4:8080")
	p.live = append(p.live, pr)

	p.ReportFailure(pr)

	if p.Stats().Alive != 1 {
		t.Errorf("alive = %d, want 1 (only direct left)", p.Stats().Alive)
	}
	if p.Next(context.Background(), time.Millisecond).IsDirect() != true {
		t.Error("the retired proxy was handed out again")
	}
}

func TestNextRespectsPacing(t *testing.T) {
	p := NewPool(5, 10, false, false, quiet)
	pr := newProxy("1.2.3.4:8080")
	p.live = append(p.live, pr)

	const interval = 200 * time.Millisecond
	first := p.Next(context.Background(), interval)
	second := p.Next(context.Background(), interval)
	if first != second {
		t.Fatal("with one proxy, Next should return the same member twice")
	}

	// The second claim must have been pushed out by the interval.
	pr.mu.Lock()
	nextAt := pr.nextAt
	pr.mu.Unlock()
	if !nextAt.After(time.Now().Add(interval - 50*time.Millisecond)) {
		t.Errorf("nextAt = %v, want it at least %v in the future", nextAt, interval)
	}
}

func TestDirectAddrIsNotMistakenForAProxy(t *testing.T) {
	d := newDirectProxy()
	if !d.IsDirect() {
		t.Error("newDirectProxy().IsDirect() = false")
	}
	if d.Addr() == "" {
		t.Error("Addr() is empty; diagnostics need a label")
	}
	if d.Client() == nil {
		t.Error("Client() = nil, want a usable client")
	}
}

func TestStatsReflectsRetirements(t *testing.T) {
	p := NewPool(3, 10, false, false, quiet)
	a, b := newProxy("1.1.1.1:80"), newProxy("2.2.2.2:80")
	p.live = append(p.live, a, b)

	p.ReportFailure(a)
	p.ReportFailure(b)

	s := p.Stats()
	if s.Alive != 0 {
		t.Errorf("Alive = %d, want 0", s.Alive)
	}
	if s.Failures != 2 {
		t.Errorf("Failures = %d, want 2", s.Failures)
	}
}

// Seeding the local IP into the live set must not make the pool look ready:
// callers wait on ProxiesAlive, and counting direct there would defeat the
// pool wait entirely.
func TestStatsSeparatesProxiesFromDirect(t *testing.T) {
	p := NewPool(5, 10, false, true, quiet)
	if got := p.Stats().ProxiesAlive; got != 0 {
		t.Errorf("ProxiesAlive = %d, want 0 before anything is probed", got)
	}

	p.live = append(p.live, newProxy("1.2.3.4:8080"))
	s := p.Stats()
	if s.ProxiesAlive != 1 {
		t.Errorf("ProxiesAlive = %d, want 1", s.ProxiesAlive)
	}
	if s.Alive != 2 {
		t.Errorf("Alive = %d, want 2 (one proxy plus direct)", s.Alive)
	}
}

func TestProxiesAliveExcludesRetired(t *testing.T) {
	p := NewPool(5, 10, false, false, quiet)
	pr := newProxy("1.2.3.4:8080")
	p.live = append(p.live, pr)
	p.ReportFailure(pr)
	if got := p.Stats().ProxiesAlive; got != 0 {
		t.Errorf("ProxiesAlive = %d, want 0 after retirement", got)
	}
}
