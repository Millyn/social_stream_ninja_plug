package translation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"socialstream-deepseek-overlay/internal/config"
)

type Service struct {
	get   func() config.Config
	mu    sync.Mutex
	cache map[string]string
}

func New(get func() config.Config) *Service {
	return &Service{get: get, cache: make(map[string]string)}
}

func (s *Service) Translate(ctx context.Context, text string) (string, error) {
	if !shouldTranslate(text) {
		return "", nil
	}
	hash := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(hash[:])
	s.mu.Lock()
	cached := s.cache[key]
	s.mu.Unlock()
	if cached != "" {
		return cached, nil
	}
	cfg := s.get()
	if cfg.DeepSeekAPIKey == "" {
		return "", errors.New("DeepSeek API Key 尚未配置")
	}
	body, _ := json.Marshal(map[string]any{"model": cfg.DeepSeekModel, "temperature": 0.1, "max_tokens": 500, "messages": []map[string]string{{"role": "system", "content": "你是直播聊天翻译器。把用户提供的英文聊天内容准确、自然地翻译成简体中文。只输出中文译文，不要解释，不要加引号。保留用户名、URL、表情符号、代码、专有名词和原始换行。"}, {"role": "user", "content": text}}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.DeepSeekAPIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("DeepSeek HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", errors.New("DeepSeek 返回空译文")
	}
	translated := strings.TrimSpace(result.Choices[0].Message.Content)
	if translated == "" {
		return "", errors.New("DeepSeek 返回空译文")
	}
	s.mu.Lock()
	if len(s.cache) >= 500 {
		for k := range s.cache {
			delete(s.cache, k)
			break
		}
	}
	s.cache[key] = translated
	s.mu.Unlock()
	return translated, nil
}

func shouldTranslate(text string) bool {
	return len([]rune(text)) > 0 && len([]rune(strings.TrimSpace(text))) <= 1000 && len([]rune(text)) >= 2 && !containsChinese(text)
}
func containsChinese(text string) bool {
	for _, r := range text {
		if r >= 0x3400 && r <= 0x9fff || r >= 0xf900 && r <= 0xfaff {
			return true
		}
	}
	return false
}
func Timeout(cfg config.Config) time.Duration {
	return time.Duration(cfg.TranslationTimeout) * time.Second
}
