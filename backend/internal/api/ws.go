package api

import (
	"context"
	"crypto/subtle"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"echomap/internal/bus"
)

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

// hub fans Redis status messages out to all connected browser clients.
type hub struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
}

func newHub() *hub { return &hub{clients: map[*wsClient]struct{}{}} }

func (h *hub) add(c *wsClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

// count is the live WS client tally (Pillar 16 api self-report).
func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *hub) remove(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c) // never close c.send: broadcast() holds the same lock, so a
	h.mu.Unlock()        // removed client is simply never sent to again (no closed-chan panic)
}

func (h *hub) broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default: // slow client: drop this frame rather than stall the whole bus
		}
	}
}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.wsAuthorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// ponytail: origin check skipped because the token above is the real gate and the
	// dev setup is cross-origin (vite :5173 proxy -> api :8080). Tighten for same-origin prod.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	c := &wsClient{conn: conn, send: make(chan []byte, 32)}
	s.hub.add(c)
	defer s.hub.remove(c)

	// CloseRead drains inbound frames (ping/pong/close) and cancels ctx on disconnect.
	ctx := conn.CloseRead(r.Context())
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// wsAuthorized accepts a browser session cookie or, for automation, the static
// token via ?token= (browsers can't set Authorization on a WS handshake, Doc 5 §0).
func (s *server) wsAuthorized(r *http.Request) bool {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if _, _, err := s.store.SessionUser(r.Context(), c.Value); err == nil {
			return true
		}
	}
	token := r.URL.Query().Get("token")
	return s.cfg.APIToken != "" && token != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.APIToken)) == 1
}

// runStatusSubscriber relays Redis device:status messages to WS clients until ctx ends.
func (s *server) runStatusSubscriber(ctx context.Context, b *bus.Bus) {
	sub := b.SubscribeStatus(ctx)
	defer sub.Close()
	log.Printf("ws: subscribed to %s", bus.ChannelStatus)

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			s.hub.broadcast([]byte(msg.Payload))
		}
	}
}
