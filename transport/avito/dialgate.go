package avito

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/p1neappleXpress/OpenFlux/utils"
)

// dialGate paces the WebSocket handshakes of all of a transport's rooms. Avito
// refuses with 429 once an address makes too many of them, and a room that
// retries at once only keeps the limit up: the gate spaces the dials and, after
// a 429, holds every room back for a pause that doubles (5 s up to 2 min,
// longer if Avito says so) until a handshake goes through.
type dialGate struct {
	mu      sync.Mutex
	next    time.Time // the earliest the next dial may start
	hold    time.Time // no dial before this, after a 429
	backoff time.Duration
	last429 time.Time // when Avito last refused

	// file is where the pause is shared with the other Avito processes on this host (a server runs a node per channel,
	// all behind one address, all under one limit); "" keeps it to this process.
	file      string
	useEnv    bool // the file is OPENFLUX_AVITO_GATE_FILE or the default one, looked up at each use
	lastAdopt time.Time
}

func (g *dialGate) path() string {
	if g.file != "" {
		return g.file
	}
	if g.useEnv {
		return gateFile()
	}
	return ""
}

// processGate is every Avito request of this process: all channels of one node, behind one address, share one limit.
var processGate = &dialGate{useEnv: true}

// gateFile is the shared pause's file: OPENFLUX_AVITO_GATE_FILE, "off" for none, else a file in the temp directory.
func gateFile() string {
	switch v := os.Getenv("OPENFLUX_AVITO_GATE_FILE"); v {
	case "off":
		return ""
	case "":
		return filepath.Join(os.TempDir(), "openflux-avito-gate")
	default:
		return v
	}
}

const (
	dialSpacing    = 250 * time.Millisecond
	holdFirst      = 5 * time.Second
	holdMax        = 30 * time.Minute
	holdJitterFrac = 0.2
	retryAfterMax  = 10 * time.Minute
	// A good handshake resets the pause only when Avito has not refused for this long: a limit that lets one in now
	// and then is still a limit.
	calmAfter = 2 * time.Minute
	// How often a process looks at the shared file.
	adoptEvery = 500 * time.Millisecond
)

// ParseRetryAfter reads a Retry-After header (seconds, or an HTTP date), clamped to [0, retryAfterMax]; 0 if absent or bad.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	var d time.Duration
	if n, err := strconv.Atoi(v); err == nil {
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	}
	return min(max(d, 0), retryAfterMax)
}

func newDialGate() *dialGate { return &dialGate{} }

// newSharedGate is a gate that shares its pause through file.
func newSharedGate(file string) *dialGate { return &dialGate{file: file} }

// adopt takes a later pause that another process wrote to the shared file. Called with g.mu held.
func (g *dialGate) adopt() {
	path := g.path()
	if path == "" || time.Since(g.lastAdopt) < adoptEvery {
		return
	}
	g.lastAdopt = time.Now()
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var holdMs, backoffMs, lastMs int64
	if n, _ := fmt.Sscanf(string(raw), "%d %d %d", &holdMs, &backoffMs, &lastMs); n != 3 {
		return
	}
	if h := time.UnixMilli(holdMs); h.After(g.hold) {
		g.hold = h
	}
	if b := time.Duration(backoffMs) * time.Millisecond; b > g.backoff {
		g.backoff = b
	}
	if l := time.UnixMilli(lastMs); l.After(g.last429) {
		g.last429 = l
	}
}

// persist writes the pause for the other processes. Called with g.mu held; a failure only costs the sharing.
func (g *dialGate) persist() {
	path := g.path()
	if path == "" {
		return
	}
	tmp := path + ".tmp"
	data := fmt.Sprintf("%d %d %d\n", g.hold.UnixMilli(), g.backoff.Milliseconds(), g.last429.UnixMilli())
	if os.WriteFile(tmp, []byte(data), 0o666) == nil {
		_ = os.Rename(tmp, path)
	}
}

// wait blocks until this dial may start; false when ctx ended first. A pause that begins while a room waits for its
// turn holds that room back too.
func (g *dialGate) wait(ctx context.Context) bool {
	for {
		g.mu.Lock()
		g.adopt()
		now := time.Now()
		at := now
		if g.next.After(at) {
			at = g.next
		}
		if g.hold.After(at) {
			at = g.hold
		}
		d := at.Sub(now)
		if d <= 0 {
			g.next = now.Add(dialSpacing)
			g.mu.Unlock()
			return ctx.Err() == nil
		}
		// Wait for the earliest turn, then look again: a 429 may have moved the pause in the meantime.
		g.next = at.Add(dialSpacing)
		g.mu.Unlock()
		if !sleepCtx(ctx, d) {
			return false
		}
		g.mu.Lock()
		held := g.hold.After(time.Now())
		g.mu.Unlock()
		if !held {
			return ctx.Err() == nil
		}
	}
}

// limited records a 429 (retry is Avito's Retry-After, 0 when it gave none).
func (g *dialGate) limited(retry time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Before(g.hold) {
		return // another room already set this pause
	}
	if g.backoff == 0 {
		g.backoff = holdFirst
	} else {
		g.backoff = min(g.backoff*2, holdMax)
	}
	d := max(g.backoff, retry)
	d += time.Duration(float64(d) * holdJitterFrac * rand.Float64())
	g.hold = now.Add(d)
	g.last429 = now
	g.persist()
	utils.Infof("[AVITO] Avito ограничивает подключения (429): пауза %v", d.Round(time.Second))
}

// ok records a handshake that went through: the next 429 starts from the short pause again.
func (g *dialGate) ok() {
	g.mu.Lock()
	if time.Since(g.last429) >= calmAfter && g.backoff != 0 {
		g.backoff = 0
		g.persist()
	}
	g.mu.Unlock()
}
