// Command fakecups is a stand-in for cups.online: the page that creates and
// hands out rooms, and the Centrifugo WebSocket the cupsonline carrier talks
// to, on one address. It is upstream's transport/cupsonline/fakecups_test.go
// as a server of its own, so two builds of different versions can meet in it.
//
//	fakecups 127.0.0.1:18090
//
// FAKE_DROP=N drops every Nth relayed message. A carrier finds it when its
// package's baseRoomURL points at http://<addr>/live-coding/ (the NE.VPN
// end-to-end check sets it in a copy of the source, never in a build we ship).
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

type fakeSrv struct{ URL string }

type fakeCups struct {
	srv *fakeSrv

	mu           sync.Mutex
	users        map[string]string
	rooms        map[string]bool
	subs         map[string]map[*fakeConn]bool
	conns        map[*fakeConn]bool
	pageHits     map[string]int
	failPages    map[string]int
	failJoins    bool
	redirectGone bool
	pack         bool
	stall        chan struct{}
	created      int
	maxCursors   int
	dropEvery    int
	relayed      int
}

type fakeConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *fakeConn) send(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ws.WriteMessage(websocket.TextMessage, []byte(msg))
}

func main() {
	addr := os.Args[1]
	f := &fakeCups{srv: &fakeSrv{URL: "http://" + addr},
		users: map[string]string{}, rooms: map[string]bool{}, subs: map[string]map[*fakeConn]bool{},
		conns: map[*fakeConn]bool{}, pageHits: map[string]int{}, failPages: map[string]int{}}
	fmt.Sscan(os.Getenv("FAKE_DROP"), &f.dropEvery)
	mux := http.NewServeMux()
	mux.HandleFunc("/live-coding/", f.page)
	mux.HandleFunc("/sub/", f.subToken)
	mux.HandleFunc("/connection/websocket", f.centrifugo)
	go func() {
		for {
			var line string
			if _, err := fmt.Scanln(&line); err != nil {
				select {}
			}
		}
	}()
	panic(http.ListenAndServe(addr, mux))
}

var _ = strings.TrimPrefix

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (f *fakeCups) page(w http.ResponseWriter, r *http.Request) {
	var stall chan struct{}
	f.locked(func() { stall = f.stall })
	if stall != nil {
		select {
		case <-stall:
		case <-r.Context().Done():
		}
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	user := ""
	if c, err := r.Cookie("sessionid"); err == nil {
		user = f.users[c.Value]
	}
	if user == "" {
		session := newUUID()
		user = newUUID()
		f.users[session] = user
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: session, Path: "/"})
	}
	http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "csrf-" + user, Path: "/"})

	room := r.URL.Query().Get("room")
	if room != "" {
		f.pageHits[room]++
	}
	open, known := f.rooms[room]
	switch {
	case room != "" && (f.failJoins || f.failPages[room] > 0):
		f.failPages[room]--
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	case room == "" || (known && !open && f.redirectGone):
		room = newUUID()
		f.rooms[room] = true
		f.created++
	case !open:
		http.NotFound(w, r)
		return
	}
	fmt.Fprintf(w, `<html><head>
<meta name="centrifuge-connection-token" content="conn-%s">
<meta name="centrifuge-connection-url" content="%s/connection">
<meta name="centrifuge-subscription-token-url" content="%s/sub/">
</head><body><div data-room="{&quot;uuid&quot;: &quot;%s&quot;}" data-user="{&quot;uuid&quot;: &quot;%s&quot;}"></div></body></html>`,
		user, f.srv.URL, f.srv.URL, room, user)
}

func (f *fakeCups) subToken(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("csrftoken")
	if r.Method != http.MethodPost || err != nil || r.Header.Get("X-CSRFToken") != c.Value {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Channel string `json:"channel"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	json.NewEncoder(w).Encode(map[string]string{"token": "sub-" + body.Channel})
}

func (f *fakeCups) centrifugo(w http.ResponseWriter, r *http.Request) {
	ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &fakeConn{ws: ws}
	f.mu.Lock()
	f.conns[c] = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.conns, c)
		for _, s := range f.subs {
			delete(s, c)
		}
		f.mu.Unlock()
		ws.Close()
	}()

	for {
		_, raw, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if string(raw) == "{}" {
			continue
		}
		var cmd struct {
			ID        int `json:"id"`
			Connect   any `json:"connect"`
			Subscribe *struct {
				Channel string `json:"channel"`
			} `json:"subscribe"`
			RPC *struct {
				Method string `json:"method"`
				Data   struct {
					Cursors []struct {
						Row    json.Number `json:"row"`
						Column json.Number `json:"column"`
					} `json:"cursors"`
					Room string `json:"room"`
					User string `json:"user"`
				} `json:"data"`
			} `json:"rpc"`
		}
		if json.Unmarshal(raw, &cmd) != nil {
			return
		}
		switch {
		case cmd.Connect != nil:
			c.send(fmt.Sprintf(`{"id":%d,"connect":{"client":"x","ping":25,"pong":true}}`, cmd.ID))
		case cmd.Subscribe != nil:
			room := strings.TrimPrefix(cmd.Subscribe.Channel, "$shared_editor:room-")
			f.mu.Lock()
			open := f.rooms[room]
			if open {
				if f.subs[room] == nil {
					f.subs[room] = map[*fakeConn]bool{}
				}
				f.subs[room][c] = true
			}
			f.mu.Unlock()
			if !open {
				c.send(fmt.Sprintf(`{"id":%d,"error":{"code":103,"message":"permission denied"}}`, cmd.ID))
				continue
			}
			c.send(fmt.Sprintf(`{"id":%d,"subscribe":{}}`, cmd.ID))
		case cmd.RPC != nil:
			c.send(fmt.Sprintf(`{"id":%d,"rpc":{}}`, cmd.ID))
			if d := cmd.RPC.Data; cmd.RPC.Method == "shared_editor_change_cursors" && len(d.Cursors) > 0 {
				// Model the real server: a non-numeric row/column is rejected,
				// anything else is relayed verbatim and in order.
				cursors := make([]any, len(d.Cursors))
				for i, cur := range d.Cursors {
					row, errR := cur.Row.Int64()
					col, errC := cur.Column.Int64()
					if errR != nil || errC != nil {
						c.send(fmt.Sprintf(`{"id":%d,"error":{"code":400,"message":"column must be a number"}}`, cmd.ID))
						cursors = nil
						break
					}
					cursors[i] = map[string]any{"row": row, "column": col}
				}
				if cursors != nil {
					f.relay(d.Room, d.User, cursors)
				}
			}
		}
	}
}

// relay hands a cursor update to everyone in the room, the sender included,
// the way Centrifugo does.
func (f *fakeCups) relay(room, user string, cursors []any) {
	push, _ := json.Marshal(map[string]any{"push": map[string]any{
		"channel": "$shared_editor:room-" + room,
		"pub": map[string]any{"data": map[string]any{
			"type": "cursors_update",
			"payload": map[string]any{
				"user_uuid": user,
				"cursors":   cursors,
			},
		}},
	}})
	msg := string(push)

	f.mu.Lock()
	f.maxCursors = max(f.maxCursors, len(cursors))
	f.relayed++
	if f.dropEvery > 0 && f.relayed%f.dropEvery == 0 {
		f.mu.Unlock()
		return
	}
	if f.pack {
		msg = "{}\n" + msg
	}
	var to []*fakeConn
	for c := range f.subs[room] {
		to = append(to, c)
	}
	f.mu.Unlock()
	for _, c := range to {
		c.send(msg)
	}
}

// closeRoom closes a room for good and hangs up on everyone in it.
func (f *fakeCups) closeRoom(room string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rooms[room] = false
	for c := range f.subs[room] {
		c.ws.Close()
	}
	delete(f.subs, room)
}

// dropConns hangs up on everyone in a room that stays open, like a network
// blip.
func (f *fakeCups) dropConns(room string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for c := range f.subs[room] {
		c.ws.Close()
	}
}

func (f *fakeCups) locked(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}
