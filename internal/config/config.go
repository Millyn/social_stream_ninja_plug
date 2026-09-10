package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Config struct {
	SessionID          string    `json:"session_id"`
	DeepSeekAPIKey     string    `json:"-"`
	DeepSeekModel      string    `json:"deepseek_model"`
	ListenAddress      string    `json:"listen_address"`
	ShowOriginal       bool      `json:"show_original"`
	MaxMessages        int       `json:"max_messages"`
	NoAvatar           bool      `json:"no_avatar"`
	AutoStart          bool      `json:"auto_start"`
	TranslationTimeout int       `json:"translation_timeout_seconds"`
	Style              ChatStyle `json:"style"`
}

type ChatStyle struct {
	FontSize         int    `json:"font_size"`
	NameSize         int    `json:"name_size"`
	SourceSize       int    `json:"source_size"`
	TranslationSize  int    `json:"translation_size"`
	TextColor        string `json:"text_color"`
	NameColor        string `json:"name_color"`
	SourceColor      string `json:"source_color"`
	TranslationColor string `json:"translation_color"`
	BubbleColor      string `json:"bubble_color"`
	BorderColor      string `json:"border_color"`
	BorderWidth      int    `json:"border_width"`
	BorderRadius     int    `json:"border_radius"`
	BubblePadding    int    `json:"bubble_padding"`
	MessageGap       int    `json:"message_gap"`
	Shadow           bool   `json:"shadow"`
	BubbleOpacity    int    `json:"bubble_opacity"`
	BorderOpacity    int    `json:"border_opacity"`
	ShowAvatar       bool   `json:"show_avatar"`
	ShowViewerCount  bool   `json:"show_viewer_count"`
	ViewerCountColor string `json:"viewer_count_color"`
}

type Input struct {
	SessionID          string    `json:"session_id"`
	DeepSeekAPIKey     string    `json:"deepseek_api_key"`
	DeepSeekModel      string    `json:"deepseek_model"`
	ShowOriginal       bool      `json:"show_original"`
	MaxMessages        int       `json:"max_messages"`
	NoAvatar           bool      `json:"no_avatar"`
	AutoStart          bool      `json:"auto_start"`
	TranslationTimeout int       `json:"translation_timeout_seconds"`
	Style              ChatStyle `json:"style"`
}

type View struct {
	Config
	HasAPIKey bool `json:"has_api_key"`
}

type Manager struct {
	mu  sync.RWMutex
	cfg Config
}

func defaults() Config {
	return Config{
		DeepSeekModel: "deepseek-chat", ListenAddress: "0.0.0.0:3000",
		ShowOriginal: true, MaxMessages: 30, AutoStart: true, TranslationTimeout: 20,
		Style: ChatStyle{FontSize: 16, NameSize: 16, SourceSize: 11, TranslationSize: 15, TextColor: "#ffffff", NameColor: "#dddddd", SourceColor: "#aaaaaa", TranslationColor: "#ffe98a", BubbleColor: "#000000", BorderColor: "#000000", BorderWidth: 0, BorderRadius: 5, BubblePadding: 5, MessageGap: 7, Shadow: true, BubbleOpacity: 35, BorderOpacity: 0, ShowAvatar: false, ViewerCountColor: "#aaaaaa"},
	}
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(dir, "SocialStreamDeepSeekOverlay", "config.json")
}

func LoadManager() *Manager {
	cfg := defaults()
	if data, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	if key, err := os.ReadFile(filepath.Join(filepath.Dir(configPath()), "deepseek.key")); err == nil {
		cfg.DeepSeekAPIKey = strings.TrimSpace(string(key))
	}
	if cfg.ListenAddress == "" || strings.HasPrefix(cfg.ListenAddress, "127.0.0.1:") {
		cfg.ListenAddress = "0.0.0.0:3000"
	}
	m := &Manager{cfg: cfg}
	m.normalize()
	return m
}

func (m *Manager) Get() Config { m.mu.RLock(); defer m.mu.RUnlock(); return m.cfg }

func (m *Manager) Update(input Input) (Config, error) {
	current := m.Get()
	cfg := Config{SessionID: strings.TrimSpace(input.SessionID), DeepSeekAPIKey: current.DeepSeekAPIKey, DeepSeekModel: strings.TrimSpace(input.DeepSeekModel), ListenAddress: current.ListenAddress, ShowOriginal: input.ShowOriginal, MaxMessages: input.MaxMessages, NoAvatar: input.NoAvatar, AutoStart: input.AutoStart, TranslationTimeout: input.TranslationTimeout, Style: input.Style}
	if strings.TrimSpace(input.DeepSeekAPIKey) != "" {
		cfg.DeepSeekAPIKey = strings.TrimSpace(input.DeepSeekAPIKey)
	}
	if err := normalize(&cfg); err != nil {
		return Config{}, err
	}
	if err := save(cfg); err != nil {
		return Config{}, err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	return cfg, nil
}

func (m *Manager) normalize() { _ = normalize(&m.cfg) }

func normalize(cfg *Config) error {
	if cfg.DeepSeekModel == "" {
		cfg.DeepSeekModel = "deepseek-chat"
	}
	if cfg.MaxMessages < 1 || cfg.MaxMessages > 200 {
		cfg.MaxMessages = 30
	}
	if cfg.TranslationTimeout < 3 || cfg.TranslationTimeout > 120 {
		cfg.TranslationTimeout = 20
	}
	if cfg.Style.BubbleOpacity < 0 || cfg.Style.BubbleOpacity > 100 {
		cfg.Style.BubbleOpacity = 35
	}
	if cfg.Style.BorderOpacity < 0 || cfg.Style.BorderOpacity > 100 {
		cfg.Style.BorderOpacity = 0
	}
	if cfg.Style.BubbleColor == "" {
		cfg.Style.BubbleColor = "#000000"
	}
	if cfg.Style.BorderColor == "" {
		cfg.Style.BorderColor = "#000000"
	}
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "0.0.0.0:3000"
	}
	return nil
}

func save(cfg Config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	public := cfg
	public.DeepSeekAPIKey = ""
	data, err := json.MarshalIndent(public, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if cfg.DeepSeekAPIKey != "" {
		if err = os.WriteFile(filepath.Join(filepath.Dir(path), "deepseek.key"), []byte(cfg.DeepSeekAPIKey), 0600); err != nil {
			return err
		}
	}
	return nil
}

func PublicView(cfg Config) View { return View{Config: cfg, HasAPIKey: cfg.DeepSeekAPIKey != ""} }
func PublicConfig(cfg Config) map[string]any {
	return map[string]any{"session_id": cfg.SessionID, "show_original": cfg.ShowOriginal, "max_messages": cfg.MaxMessages, "no_avatar": cfg.NoAvatar, "style": cfg.Style}
}
