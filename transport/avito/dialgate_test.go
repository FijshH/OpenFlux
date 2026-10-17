package avito

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestDialGateSpacesDials(t *testing.T) {
	g := newDialGate()
	start := time.Now()
	for i := 0; i < 4; i++ {
		if !g.wait(context.Background()) {
			t.Fatal("wait cancelled")
		}
	}
	if got := time.Since(start); got < 3*dialSpacing-20*time.Millisecond {
		t.Fatalf("4 dials took %v, want about %v", got, 3*dialSpacing)
	}
}

func TestDialGateHoldsEveryRoomAfter429AndDoublesTheNextPause(t *testing.T) {
	g := newDialGate()
	g.limited(0)
	g.mu.Lock()
	first := time.Until(g.hold)
	g.mu.Unlock()
	if first < holdFirst-time.Second || first > holdFirst+2*time.Second {
		t.Fatalf("first pause %v, want about %v", first, holdFirst)
	}
	// A second 429 while the pause runs must not stack another one.
	g.limited(0)
	g.mu.Lock()
	again := time.Until(g.hold)
	g.mu.Unlock()
	if again > first+time.Second {
		t.Fatalf("a 429 during a pause extended it: %v then %v", first, again)
	}
	// The pause is over: the next 429 doubles it; a good handshake resets it.
	g.mu.Lock()
	g.hold = time.Now().Add(-time.Second)
	g.mu.Unlock()
	g.limited(0)
	g.mu.Lock()
	second := g.backoff
	g.mu.Unlock()
	if second != 2*holdFirst {
		t.Fatalf("second pause %v, want %v", second, 2*holdFirst)
	}
	// A good handshake after a calm spell resets it (right after a refusal it would not: see the next test).
	g.mu.Lock()
	g.last429 = time.Now().Add(-2 * calmAfter)
	g.mu.Unlock()
	g.ok()
	g.mu.Lock()
	reset := g.backoff
	g.mu.Unlock()
	if reset != 0 {
		t.Fatalf("a good handshake after a calm spell must reset the pause, got %v", reset)
	}
}

func TestDialGateObeysRetryAfterAndCancel(t *testing.T) {
	g := newDialGate()
	g.limited(30 * time.Second)
	g.mu.Lock()
	d := time.Until(g.hold)
	g.mu.Unlock()
	if d < 29*time.Second {
		t.Fatalf("Retry-After 30 s gave a pause of %v", d)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if g.wait(ctx) {
		t.Fatal("wait must give up when its context ends")
	}
}

func TestParseRetryAfterIsClamped(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"7", 7 * time.Second},
		{"-5", 0},
		{"999999", retryAfterMax},
		{"garbage", 0},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0},
	} {
		if got := parseRetryAfter(c.in, now); got != c.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestDialGateDoesNotResetTheBackoffOnAnIsolatedSuccess(t *testing.T) {
	g := newDialGate()
	g.limited(0)
	g.ok() // a success right after a refusal: the limit is still there
	g.mu.Lock()
	b := g.backoff
	g.mu.Unlock()
	if b != holdFirst {
		t.Fatalf("one good handshake right after a 429 reset the pause to %v", b)
	}
	g.mu.Lock()
	g.last429 = time.Now().Add(-2 * calmAfter)
	g.mu.Unlock()
	g.ok()
	g.mu.Lock()
	b = g.backoff
	g.mu.Unlock()
	if b != 0 {
		t.Fatalf("a calm spell must reset the pause, got %v", b)
	}
}

func TestDialGateHoldsARoomThatWasAlreadyQueued(t *testing.T) {
	g := newDialGate()
	g.wait(context.Background()) // takes the first turn
	done := make(chan time.Time, 1)
	go func() {
		g.wait(context.Background()) // queued behind it
		done <- time.Now()
	}()
	time.Sleep(50 * time.Millisecond)
	g.mu.Lock()
	g.hold = time.Now().Add(400 * time.Millisecond)
	g.mu.Unlock()
	start := time.Now()
	select {
	case at := <-done:
		if at.Sub(start) < 300*time.Millisecond {
			t.Fatalf("a queued room dialled %v after a 429 set a 400 ms pause", at.Sub(start))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the queued room never came out of its wait")
	}
}
