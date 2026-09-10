package stream

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"socialstream-deepseek-overlay/internal/config"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]struct{}
}

func NewHub() *Hub                      { return &Hub{clients: make(map[*websocket.Conn]struct{})} }
func (h *Hub) Add(c *websocket.Conn)    { h.mu.Lock(); h.clients[c] = struct{}{}; h.mu.Unlock() }
func (h *Hub) Remove(c *websocket.Conn) { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }
func (h *Hub) Broadcast(message []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if err := c.WriteMessage(websocket.TextMessage, message); err != nil {
			_ = c.Close()
			delete(h.clients, c)
		}
	}
}
func (h *Hub) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"clear"}`))
	}
}

func Run(get func() config.Config, hub *Hub) {
	for {
		cfg := get()
		if cfg.SessionID == "" || !cfg.AutoStart {
			time.Sleep(2 * time.Second)
			continue
		}
		url := "wss://io.socialstream.ninja/join/" + cfg.SessionID + "/4"
		connection, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			log.Printf("Social Stream Ninja connected (session %s, channel 4)", cfg.SessionID)
			for {
				_, message, readErr := connection.ReadMessage()
				if readErr != nil {
					break
				}
				if payload, ok := Normalize(message); ok {
					hub.Broadcast(payload)
				}
			}
			_ = connection.Close()
		} else {
			log.Printf("Social Stream connection: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func Normalize(raw []byte) ([]byte, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	for i := 0; i < 3; i++ {
		obj, ok := value.(map[string]any)
		if !ok {
			break
		}
		if s, ok := obj["value"].(string); ok {
			var nested any
			if json.Unmarshal([]byte(s), &nested) == nil {
				value = nested
				continue
			}
		}
		if nested, ok := obj["data"]; ok {
			if m, ok := nested.(map[string]any); ok && (len(obj) == 1 || !hasChatFields(obj)) {
				value = m
				continue
			}
		}
		break
	}
	if obj, ok := value.(map[string]any); ok {
		for _, key := range []string{"viewer_count", "viewerCount", "viewers", "viewers_count", "viewer_count_total", "counterValue", "totalViewers", "viewerCountTotal"} {
			if n, ok := numberValue(obj[key]); ok {
				obj["viewer_count"] = n
				break
			}
		}
		for _, containerKey := range []string{"meta", "metadata", "payload"} {
			container, ok := obj[containerKey].(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"viewer_count", "viewerCount", "viewers", "viewers_count", "count", "total", "totalViewers"} {
				if n, ok := numberValue(container[key]); ok {
					obj["viewer_count"] = n
					break
				}
			}
		}
	}
	output, err := json.Marshal(value)
	return output, err == nil
}
func hasChatFields(obj map[string]any) bool {
	for _, key := range []string{"chatmessage", "chatname", "event", "hasDonation", "donation", "membership", "subtitle"} {
		if _, ok := obj[key]; ok {
			return true
		}
	}
	return false
}
func numberValue(value any) (int, bool) {
	switch n := value.(type) {
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, e := strconv.Atoi(string(n))
		return i, e == nil
	case string:
		i, e := strconv.Atoi(strings.TrimSpace(strings.ReplaceAll(n, ",", "")))
		return i, e == nil
	}
	return 0, false
}
