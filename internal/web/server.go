package web

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"socialstream-deepseek-overlay/internal/config"
	"socialstream-deepseek-overlay/internal/stream"
	"socialstream-deepseek-overlay/internal/translation"
	"strconv"
	"strings"
)

type Server struct {
	manager    *config.Manager
	translator *translation.Service
	hub        *stream.Hub
}

func New(manager *config.Manager, translator *translation.Service) *Server {
	return &Server{manager: manager, translator: translator, hub: stream.NewHub()}
}
func (s *Server) ListenAndServe(address string) error {
	assets := http.FileServer(http.FS(publicFS()))
	mux := http.NewServeMux()
	mux.Handle("/assets/", http.StripPrefix("/assets/", assets))
	mux.HandleFunc("/", s.overlay)
	mux.HandleFunc("/settings", s.settings)
	mux.HandleFunc("/api/config", s.config)
	mux.HandleFunc("/api/translate", s.translate)
	mux.HandleFunc("/api/test", s.test)
	mux.HandleFunc("/api/ninja-style", s.ninjaStyle)
	mux.HandleFunc("/api/clear", s.clear)
	mux.HandleFunc("/ws", s.ws)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"ok": true}) })
	go stream.Run(s.manager.Get, s.hub)
	return http.ListenAndServe(address, mux)
}
func (s *Server) overlay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data, _ := fs.ReadFile(publicFS(), "overlay.html")
	_, _ = w.Write(data)
}
func (s *Server) settings(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data, _ := fs.ReadFile(publicFS(), "settings.html")
	_, _ = w.Write(data)
}
func (s *Server) config(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, config.PublicView(s.manager.Get()))
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input config.Input
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid config: " + err.Error()})
		return
	}
	cfg, err := s.manager.Update(input)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config.PublicView(cfg))
}
func (s *Server) translate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid text"})
		return
	}
	cfg := s.manager.Get()
	ctx, cancel := context.WithTimeout(r.Context(), translation.Timeout(cfg))
	defer cancel()
	translated, err := s.translator.Translate(ctx, strings.TrimSpace(in.Text))
	response := map[string]any{"translated": translated, "skipped": translated == ""}
	if err != nil {
		response["error"] = err.Error()
		writeJSON(w, http.StatusBadGateway, response)
		return
	}
	writeJSON(w, http.StatusOK, response)
}
func (s *Server) test(w http.ResponseWriter, r *http.Request) {
	cfg := s.manager.Get()
	ctx, cancel := context.WithTimeout(r.Context(), translation.Timeout(cfg))
	defer cancel()
	translated, err := s.translator.Translate(ctx, "Hello, this is a DeepSeek translation test.")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"translated": translated})
}
func (s *Server) clear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	s.hub.Clear()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	c, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.Add(c)
	defer s.hub.Remove(c)
	defer c.Close()
	for {
		if _, _, err = c.ReadMessage(); err != nil {
			return
		}
	}
}
func (s *Server) ninjaStyle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	style, err := importNinjaStyle(in.URL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, style)
}
func importNinjaStyle(raw string) (config.ChatStyle, error) {
	cfg := config.ChatStyle{FontSize: 16, NameSize: 16, SourceSize: 11, TranslationSize: 15, TextColor: "#ffffff", NameColor: "#dddddd", SourceColor: "#aaaaaa", TranslationColor: "#ffe98a", BubbleColor: "#000000", BorderColor: "#000000", BorderOpacity: 0, BubbleOpacity: 35, Shadow: true, ViewerCountColor: "#aaaaaa"}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return cfg, errors.New("URL 格式无效")
	}
	q := u.Query()
	if _, ok := q["darkmode"]; ok && q.Get("darkmode") != "false" {
		cfg.BubbleColor = "#000000"
	}
	if q.Get("darkmode") == "false" {
		cfg.BubbleColor = "#ffffff"
		cfg.TextColor = "#111111"
		cfg.NameColor = "#222222"
		cfg.SourceColor = "#555555"
		cfg.TranslationColor = "#8a5a00"
	}
	if _, ok := q["noavatar"]; ok {
		cfg.ShowAvatar = q.Get("noavatar") == "false"
	}
	if n, e := strconv.Atoi(q.Get("padding")); e == nil && n >= 0 && n <= 40 {
		cfg.BubblePadding = n
	}
	if scale, e := strconv.ParseFloat(q.Get("scale"), 64); e == nil && scale >= .5 && scale <= 3 {
		cfg.FontSize = int(16 * scale)
		cfg.NameSize = int(16 * scale)
		cfg.TranslationSize = int(15 * scale)
	}
	if _, ok := q["compact"]; ok && q.Get("compact") != "false" {
		cfg.MessageGap = 2
		if cfg.BubblePadding > 3 {
			cfg.BubblePadding = 3
		}
	}
	if v := q.Get("chroma"); len(v) == 4 {
		if n, e := strconv.ParseUint(v, 16, 16); e == nil {
			cfg.BubbleColor = "#" + v[:2] + v[:2] + v[:2]
			cfg.BubbleOpacity = int(n&255) * 100 / 255
		}
	}
	return cfg, nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
