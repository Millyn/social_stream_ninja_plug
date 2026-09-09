package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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
}

type ConfigInput struct {
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

type ConfigView struct {
	Config
	HasAPIKey bool `json:"has_api_key"`
}

func defaultConfig() Config {
	return Config{
		DeepSeekModel: "deepseek-chat", ListenAddress: "0.0.0.0:3000",
		ShowOriginal: true, MaxMessages: 30, AutoStart: true, TranslationTimeout: 20,
		Style: ChatStyle{FontSize: 16, NameSize: 16, SourceSize: 11, TranslationSize: 15, TextColor: "#ffffff", NameColor: "#dddddd", SourceColor: "#aaaaaa", TranslationColor: "#ffe98a", BubbleColor: "#00000059", BorderColor: "#00000000", BorderWidth: 0, BorderRadius: 5, BubblePadding: 5, MessageGap: 7, Shadow: true},
	}
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(dir, "SocialStreamDeepSeekOverlay", "config.json")
}

func loadConfig() Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	secret, secretErr := os.ReadFile(filepath.Join(filepath.Dir(configPath()), "deepseek.key"))
	if secretErr == nil {
		cfg.DeepSeekAPIKey = strings.TrimSpace(string(secret))
	}
	if cfg.ListenAddress == "" || strings.HasPrefix(cfg.ListenAddress, "127.0.0.1:") {
		cfg.ListenAddress = "0.0.0.0:3000"
	}
	return cfg
}

func saveConfig(cfg Config) error {
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

type App struct {
	mu        sync.RWMutex
	cfg       Config
	cache     map[string]string
	clients   map[*websocket.Conn]struct{}
	clientsMu sync.Mutex
}

var chineseRE = regexp.MustCompile(`[\x{3400}-\x{4DBF}\x{4E00}-\x{9FFF}\x{F900}-\x{FAFF}]`)
var englishRE = regexp.MustCompile(`[A-Za-z]`)

func shouldTranslate(text string) bool {
	letters := len(englishRE.FindAllString(text, -1))
	return letters >= 2 && !chineseRE.MatchString(text)
}

func (a *App) getConfig() Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg
}

func (a *App) updateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.DeepSeekModel) == "" {
		cfg.DeepSeekModel = "deepseek-chat"
	}
	if cfg.MaxMessages < 1 || cfg.MaxMessages > 200 {
		cfg.MaxMessages = 30
	}
	if cfg.TranslationTimeout < 3 || cfg.TranslationTimeout > 120 {
		cfg.TranslationTimeout = 20
	}
	if strings.TrimSpace(cfg.ListenAddress) == "" {
		cfg.ListenAddress = "0.0.0.0:3000"
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.mu.Unlock()
	return nil
}

func (a *App) translate(ctx context.Context, text string) (string, error) {
	if !shouldTranslate(text) {
		return "", nil
	}
	hash := sha256.Sum256([]byte(text))
	key := hex.EncodeToString(hash[:])
	a.mu.RLock()
	cached, cfg := a.cache[key], a.cfg
	a.mu.RUnlock()
	if cached != "" {
		return cached, nil
	}
	if cfg.DeepSeekAPIKey == "" {
		return "", errors.New("DeepSeek API Key 尚未配置")
	}

	requestBody := map[string]any{
		"model": cfg.DeepSeekModel, "temperature": 0.1, "max_tokens": 500,
		"messages": []map[string]string{
			{"role": "system", "content": "你是直播聊天翻译器。把用户提供的英文聊天内容准确、自然地翻译成简体中文。只输出中文译文，不要解释，不要加引号。保留用户名、URL、表情符号、代码、专有名词和原始换行。"},
			{"role": "user", "content": text},
		},
	}
	body, _ := json.Marshal(requestBody)
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
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", errors.New("DeepSeek 返回空译文")
	}
	translated := strings.TrimSpace(result.Choices[0].Message.Content)
	a.mu.Lock()
	a.cache[key] = translated
	a.mu.Unlock()
	return translated, nil
}

func (a *App) translateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&input) != nil {
		http.Error(w, "invalid text", http.StatusBadRequest)
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" || len([]rune(input.Text)) > 1000 {
		http.Error(w, "invalid text", http.StatusBadRequest)
		return
	}
	cfg := a.getConfig()
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.TranslationTimeout)*time.Second)
	defer cancel()
	translated, err := a.translate(ctx, input.Text)
	response := map[string]any{"translated": translated, "skipped": translated == ""}
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		response["error"] = err.Error()
		w.WriteHeader(http.StatusBadGateway)
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (a *App) configHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		cfg := a.getConfig()
		_ = json.NewEncoder(w).Encode(ConfigView{Config: cfg, HasAPIKey: cfg.DeepSeekAPIKey != ""})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var input ConfigInput
	if json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&input) != nil {
		http.Error(w, "invalid config", http.StatusBadRequest)
		return
	}
	current := a.getConfig()
	cfg := Config{SessionID: strings.TrimSpace(input.SessionID), DeepSeekAPIKey: current.DeepSeekAPIKey, DeepSeekModel: strings.TrimSpace(input.DeepSeekModel), ListenAddress: current.ListenAddress, ShowOriginal: input.ShowOriginal, MaxMessages: input.MaxMessages, NoAvatar: input.NoAvatar, AutoStart: input.AutoStart, TranslationTimeout: input.TranslationTimeout, Style: input.Style}
	if strings.TrimSpace(input.DeepSeekAPIKey) != "" {
		cfg.DeepSeekAPIKey = strings.TrimSpace(input.DeepSeekAPIKey)
	}
	if err := a.updateConfig(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(ConfigView{Config: cfg, HasAPIKey: cfg.DeepSeekAPIKey != ""})
}

func (a *App) testHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg := a.getConfig()
	if cfg.DeepSeekAPIKey == "" {
		http.Error(w, "DeepSeek API Key 尚未配置", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.TranslationTimeout)*time.Second)
	defer cancel()
	translated, err := a.translate(ctx, "Hello, this is a DeepSeek translation test.")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"translated": translated})
}

var upgrader = websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}

func (a *App) localWebSocket(w http.ResponseWriter, r *http.Request) {
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.clientsMu.Lock()
	a.clients[connection] = struct{}{}
	a.clientsMu.Unlock()
	defer func() {
		a.clientsMu.Lock()
		delete(a.clients, connection)
		a.clientsMu.Unlock()
		_ = connection.Close()
	}()
	for {
		if _, _, err := connection.ReadMessage(); err != nil {
			return
		}
	}
}

func (a *App) broadcast(message []byte) {
	a.clientsMu.Lock()
	defer a.clientsMu.Unlock()
	for client := range a.clients {
		if err := client.WriteMessage(websocket.TextMessage, message); err != nil {
			_ = client.Close()
			delete(a.clients, client)
		}
	}
}

func (a *App) socialStreamLoop() {
	for {
		cfg := a.getConfig()
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
				if payload, ok := normalizeSocialStreamMessage(message); ok {
					a.broadcast(payload)
				}
			}
			_ = connection.Close()
		}
		if err != nil {
			log.Printf("Social Stream connection: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func normalizeSocialStreamMessage(raw []byte) ([]byte, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	// Channel 4 normally sends the message object directly. Keep every valid object.
	if nested, ok := obj["value"].(string); ok {
		var inner any
		if json.Unmarshal([]byte(nested), &inner) == nil {
			value = inner
		}
	}
	if nested, ok := value.(map[string]any)["data"].(map[string]any); ok {
		if len(value.(map[string]any)) == 1 {
			value = nested
		}
	}
	output, err := json.Marshal(value)
	return output, err == nil
}

func publicConfig(cfg Config) map[string]any {
	return map[string]any{"session_id": cfg.SessionID, "show_original": cfg.ShowOriginal, "max_messages": cfg.MaxMessages, "no_avatar": cfg.NoAvatar, "style": cfg.Style}
}

func renderOverlayHTML(cfg map[string]any) string {
	data, _ := json.Marshal(cfg)
	html := strings.Replace(overlayHTML, "{{json .}}", string(data), 1)
	html = strings.Replace(html, "{{json .}}", string(data), 1)
	return html
}

func (a *App) overlayHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderOverlayHTML(publicConfig(a.getConfig()))))
}

func (a *App) settingsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(settingsTemplate))
}

func (a *App) run() {
	cfg := a.getConfig()
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.overlayHandler)
	mux.HandleFunc("/settings", a.settingsHandler)
	mux.HandleFunc("/ws", a.localWebSocket)
	mux.HandleFunc("/api/config", a.configHandler)
	mux.HandleFunc("/api/translate", a.translateHandler)
	mux.HandleFunc("/api/test", a.testHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"ok":true}`)) })
	go a.socialStreamLoop()
	log.Printf("设置页面: http://%s/settings", cfg.ListenAddress)
	log.Printf("Overlay: http://%s/?session=%s", cfg.ListenAddress, cfg.SessionID)
	log.Fatal(http.ListenAndServe(cfg.ListenAddress, mux))
}

const overlayHTML = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Social Stream DeepSeek Overlay</title><style>
*{box-sizing:border-box}html,body{margin:0;width:100%;height:100%;overflow:hidden;background:transparent}body{color:var(--text-color,#fff);font:var(--font-size,16px)/1.45 system-ui,-apple-system,"Segoe UI",sans-serif;text-shadow:var(--shadow,1px 1px 3px #000,-1px -1px 3px #000)}#output{position:fixed;inset:0;padding:14px;display:flex;flex-direction:column;justify-content:flex-end;gap:var(--message-gap,7px);overflow:hidden}.message{display:flex;gap:8px;align-items:flex-start;max-width:100%;animation:appear .2s ease-out}.avatar{width:28px;height:28px;border-radius:50%;object-fit:cover;flex:0 0 auto}.bubble{min-width:0;padding:var(--bubble-padding,5px);background:var(--bubble-color,#00000059);border:var(--border-width,0px) solid var(--border-color,#0000);border-radius:var(--border-radius,5px)}.name{font-size:var(--name-size,16px);font-weight:700;color:var(--name-color,#ddd);margin-right:6px}.source{font-size:var(--source-size,11px);color:var(--source-color,#aaa);opacity:.85;margin-right:5px}.original{display:block;color:var(--text-color,#fff);font-size:var(--font-size,16px)}.translation{display:block;color:var(--translation-color,#ffe98a);font-size:var(--translation-size,15px);border-left:2px solid currentColor;padding-left:7px;margin-top:2px}.translation.pending{color:#aaa;border-color:#888}.translation.error{color:#ff9b9b;border-color:#ff6b6b}.event{font-size:var(--font-size,16px);color:var(--event-color,#ffd36a);font-weight:600}.event .event-label{font-size:var(--name-size,16px);color:var(--name-color,#ddd)}#status{position:fixed;right:6px;top:4px;color:#aaa;font:11px/1 sans-serif;opacity:.5}@keyframes appear{from{opacity:0;transform:translateY(8px)}to{opacity:1;transform:translateY(0)}}
</style></head><body><main id="output" aria-live="polite"></main><div id="status">正在连接…</div><script>
const cfg={{json .}},s=cfg.style||{},p=new URLSearchParams(location.search),session=p.get('session')||cfg.session_id,out=document.querySelector('#output'),status=document.querySelector('#status'),max=Number(p.get('limit')||cfg.max_messages||30),showOriginal=p.has('original')||cfg.show_original;document.documentElement.style.setProperty('--font-size',(s.font_size||16)+'px');document.documentElement.style.setProperty('--name-size',(s.name_size||16)+'px');document.documentElement.style.setProperty('--source-size',(s.source_size||11)+'px');document.documentElement.style.setProperty('--translation-size',(s.translation_size||15)+'px');document.documentElement.style.setProperty('--text-color',s.text_color||'#fff');document.documentElement.style.setProperty('--name-color',s.name_color||'#ddd');document.documentElement.style.setProperty('--source-color',s.source_color||'#aaa');document.documentElement.style.setProperty('--translation-color',s.translation_color||'#ffe98a');document.documentElement.style.setProperty('--event-color',s.event_color||'#ffd36a');document.documentElement.style.setProperty('--bubble-color',s.bubble_color||'#00000059');document.documentElement.style.setProperty('--border-color',s.border_color||'#0000');document.documentElement.style.setProperty('--border-width',(s.border_width||0)+'px');document.documentElement.style.setProperty('--border-radius',(s.border_radius??5)+'px');document.documentElement.style.setProperty('--bubble-padding',(s.bubble_padding??5)+'px');document.documentElement.style.setProperty('--message-gap',(s.message_gap??7)+'px');if(!s.shadow)document.documentElement.style.setProperty('--shadow','none');
const cn=t=>/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/u.test(t),en=t=>(t.match(/[A-Za-z]/g)||[]).length>=2&&!cn(t);function text(x){return String(x??'').replace(/[\u0000-\u001f]/g,' ').trim()}function eventText(d){let e=text(d.event),m=text(d.chatmessage),don=text(d.hasDonation||d.donation||d.donoValue),mem=text(d.membership||d.subtitle);if(m)return m;if(e||don||mem){let labels={subscription:'订阅',resub:'续订',subscription_gift:'订阅礼物',gift:'礼物',raid:'出团/突袭',follow:'关注',cheer:'Bits 加油',donation:'打赏',superchat:'超级留言',sub:'订阅'};let label=labels[e]||e||'直播事件';let parts=[label];if(d.chatname)parts.push('来自 '+text(d.chatname));if(don)parts.push('金额：'+don);if(mem)parts.push(mem);return parts.join(' · ')}return ''}async function tr(t,node){if(!en(t)||cn(t))return;node.textContent='翻译中…';node.classList.add('pending');try{let r=await fetch('/api/translate',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:t})}),j=await r.json();if(!r.ok)throw Error(j.error||'翻译失败');if(j.skipped){node.remove();return}node.textContent=j.translated;node.classList.remove('pending')}catch(e){node.textContent='翻译失败';node.title=e.message;node.classList.remove('pending');node.classList.add('error')}}function add(d){if(!d||typeof d!=='object')return;let t=text(d.chatmessage),n=text(d.chatname),et=eventText(d),isEvent=!!d.event||!!d.hasDonation||!!d.donation||!!d.donoValue||!!d.membership||!!d.subtitle;if(!t&&!et&&!n)return;let row=document.createElement('article');row.className='message';if(d.nameColor)row.style.setProperty('--name-color',d.nameColor);if(d.chatimg&&!cfg.no_avatar&&!p.has('noavatar')){let a=document.createElement('img');a.className='avatar';a.src=d.chatimg;a.alt='';a.onerror=()=>a.remove();row.append(a)}let b=document.createElement('div');b.className='bubble';let h=document.createElement('div'),name=document.createElement('span');name.className='name';name.textContent=n||'Social Stream';h.append(name);if(d.type){let q=document.createElement('span');q.className='source';q.textContent='['+d.type+']';h.append(q)}b.append(h);if(isEvent&&!t){let q=document.createElement('div');q.className='event';q.textContent=et;b.append(q)}if(t){let o=document.createElement('span');o.className='original';o.textContent=t;o.hidden=!showOriginal;b.append(o);let q=document.createElement('span');q.className='translation';b.append(q);tr(t,q)}row.append(b);out.append(row);while(out.querySelectorAll('.message').length>max)out.querySelector('.message')?.remove()}function connect(){if(!session){status.textContent='请先打开设置页面填写 Session ID';return}let endpoint='wss://io.socialstream.ninja/join/'+encodeURIComponent(session)+'/4';status.textContent='正在连接 Social Stream Ninja…';ws=new WebSocket(endpoint);ws.onopen=()=>{status.textContent='Social Stream Ninja 已连接，DeepSeek 翻译已启用'};ws.onmessage=e=>{try{let d=JSON.parse(e.data);if(typeof d.value==='string')try{d=JSON.parse(d.value)}catch{}if(d&&d.data&&typeof d.data==='object'&&!d.chatmessage&&!d.event)d=d.data;add(d)}catch{}};ws.onerror=()=>{status.textContent='Social Stream Ninja WSS 连接失败'};ws.onclose=()=>{status.textContent='连接断开，重连中…';clearTimeout(reconnect);reconnect=setTimeout(connect,3000)}}let ws,reconnect;if(session)connect();
</script></body></html>`

const settingsTemplate = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Social Stream DeepSeek 设置</title><style>
*{box-sizing:border-box}body{margin:0;background:#17191f;color:#e9edf5;font:15px system-ui,-apple-system,"Segoe UI",sans-serif}main{width:min(900px,calc(100% - 32px));margin:28px auto}.card{background:#242832;border:1px solid #343b49;border-radius:12px;padding:22px;margin-top:18px}h1{margin:0 0 8px}p{color:#aeb7c8}.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:12px 16px}label{display:block;margin:12px 0 6px;font-weight:600}input[type=text],input[type=password],input[type=number],input[type=color]{width:100%;padding:9px 10px;border-radius:7px;border:1px solid #4a5363;background:#171a20;color:#fff;font-size:14px}input[type=color]{height:38px;padding:3px}input[type=checkbox]{margin-right:8px;accent-color:#5c9dff}button{border:0;border-radius:7px;padding:10px 15px;margin:16px 8px 0 0;background:#397eea;color:#fff;font-weight:600;cursor:pointer}button.secondary{background:#414a59}.hint{font-size:13px;color:#9da8b8}.section-title{font-size:18px;margin:0 0 6px;color:#80b7ff}.preview{min-height:150px;padding:18px;background:linear-gradient(135deg,#1b2333,#101319);border-radius:8px;display:flex;align-items:flex-end}.preview-bubble{padding:var(--pad);background:var(--bubble);border:var(--bw) solid var(--border);border-radius:var(--radius);color:var(--text);box-shadow:var(--shadow)}.preview-name{font-size:var(--name-size);font-weight:700;color:var(--name)}.preview-source{font-size:var(--source-size);color:var(--source);margin-left:6px}.preview-original{font-size:var(--font-size)}.preview-translation{font-size:var(--trans-size);color:var(--trans);border-left:2px solid currentColor;padding-left:6px;margin-top:3px}@media(max-width:700px){.grid{grid-template-columns:1fr 1fr}}@media(max-width:480px){.grid{grid-template-columns:1fr}}
</style></head><body><main><h1>Social Stream Ninja · DeepSeek 翻译</h1><p>这里可以设置连接、翻译以及聊天显示样式。样式保存后，已打开的 Overlay 刷新即可生效。</p><section class="card"><h2 class="section-title">连接与翻译</h2><form id="form"><div class="grid"><div><label>Session ID</label><input id="session_id" type="text" required></div><div><label>DeepSeek API Key</label><input id="api_key" type="password" placeholder="留空保持当前 Key"></div><div><label>DeepSeek 模型</label><input id="model" type="text"></div><div><label>翻译超时（秒）</label><input id="timeout" type="number" min="3" max="120"></div><div><label>最多显示消息数</label><input id="max" type="number" min="1" max="200"></div></div><div class="hint" id="key_state"></div><label><input id="original" type="checkbox">显示英文原文</label><label><input id="avatar" type="checkbox">显示用户头像</label><label><input id="autostart" type="checkbox">自动连接 Social Stream Ninja</label></section><section class="card"><h2 class="section-title">聊天样式</h2><div class="grid"><div><label>正文大小（px）</label><input id="font_size" type="number" min="8" max="48"></div><div><label>昵称大小（px）</label><input id="name_size" type="number" min="8" max="48"></div><div><label>来源大小（px）</label><input id="source_size" type="number" min="8" max="32"></div><div><label>译文大小（px）</label><input id="translation_size" type="number" min="8" max="48"></div><div><label>气泡内边距（px）</label><input id="bubble_padding" type="number" min="0" max="40"></div><div><label>消息间距（px）</label><input id="message_gap" type="number" min="0" max="40"></div><div><label>边框宽度（px）</label><input id="border_width" type="number" min="0" max="10"></div><div><label>边框圆角（px）</label><input id="border_radius" type="number" min="0" max="40"></div></div><div class="grid"><div><label>正文颜色</label><input id="text_color" type="color"></div><div><label>昵称颜色</label><input id="name_color" type="color"></div><div><label>来源颜色</label><input id="source_color" type="color"></div><div><label>译文颜色</label><input id="translation_color" type="color"></div><div><label>气泡背景色</label><input id="bubble_color" type="color"></div><div><label>边框颜色</label><input id="border_color" type="color"></div></div><label><input id="shadow" type="checkbox">启用文字阴影</label><div class="preview"><div class="preview-bubble" id="preview"><span class="preview-name">主播昵称</span><span class="preview-source">[twitch]</span><div class="preview-original">This is an English chat message</div><div class="preview-translation">这是一条英文聊天消息</div></div></div><button type="submit">保存全部设置</button><button type="button" class="secondary" id="test">测试 DeepSeek</button><div id="message"></div></form></section><section class="card"><b>OBS Browser Source 地址</b><p id="overlay_url">保存 Session ID 后生成</p><button type="button" class="secondary" id="open_overlay">打开 Overlay 预览</button></section></main><script>
const $=id=>document.getElementById(id),form=$("form"),msg=$("message"),styleKeys=["font_size","name_size","source_size","translation_size","text_color","name_color","source_color","translation_color","bubble_color","border_color","border_width","border_radius","bubble_padding","message_gap","shadow"];let cfg;function set(id,v){$(id).value=v}function updatePreview(){let p=$("preview"),v=id=>$(id).value;p.style.setProperty("--font-size",v("font_size")+"px");p.style.setProperty("--name-size",v("name_size")+"px");p.style.setProperty("--source-size",v("source_size")+"px");p.style.setProperty("--trans-size",v("translation_size")+"px");p.style.setProperty("--pad",v("bubble_padding")+"px");p.style.setProperty("--bw",v("border_width")+"px");p.style.setProperty("--radius",v("border_radius")+"px");p.style.setProperty("--text",v("text_color"));p.style.setProperty("--name",v("name_color"));p.style.setProperty("--source",v("source_color"));p.style.setProperty("--trans",v("translation_color"));p.style.setProperty("--bubble",v("bubble_color"));p.style.setProperty("--border",v("border_color"));p.style.setProperty("--shadow",$("shadow").checked?"0 1px 3px #000":"none")}function updateURL(){let s=$("session_id").value.trim();$("overlay_url").textContent=s?location.origin+"/?session="+encodeURIComponent(s)+"&noavatar":"保存 Session ID 后生成"}async function load(){let r=await fetch("/api/config"),j=await r.json(),s=j.style||{};cfg=j;set("session_id",j.session_id||"");set("model",j.deepseek_model||"deepseek-chat");set("timeout",j.translation_timeout_seconds||20);set("max",j.max_messages||30);$("original").checked=!!j.show_original;$("avatar").checked=!j.no_avatar;$("autostart").checked=j.auto_start!==false;styleKeys.forEach(k=>{if(k==="shadow")$(k).checked=s[k]!==false;else set(k,s[k]??"")});$("key_state").textContent=j.has_api_key?"当前已配置 API Key":"当前尚未配置 API Key";updateURL();updatePreview()}form.addEventListener("submit",async e=>{e.preventDefault();let st={};styleKeys.forEach(k=>st[k]=k==="shadow"?$(k).checked:Number($(k).value)||0);["text_color","name_color","source_color","translation_color","bubble_color","border_color"].forEach(k=>st[k]=$(k).value);let body={session_id:$("session_id").value,deepseek_api_key:$("api_key").value,deepseek_model:$("model").value,translation_timeout_seconds:Number($("timeout").value),max_messages:Number($("max").value),show_original:$("original").checked,no_avatar:!$("avatar").checked,auto_start:$("autostart").checked,style:st};let r=await fetch("/api/config",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)}),j=await r.json();if(!r.ok){msg.textContent=j.error||"保存失败";return}cfg=j;$("api_key").value="";$("key_state").textContent=j.has_api_key?"当前已配置 API Key":"当前尚未配置 API Key";msg.textContent="全部设置已保存。请刷新 Overlay 页面。";updateURL()});styleKeys.forEach(k=>$(k).addEventListener("input",updatePreview));$("session_id").addEventListener("input",updateURL);$("test").onclick=async()=>{msg.textContent="测试中…";let r=await fetch("/api/test",{method:"POST"}),j=await r.json();msg.textContent=r.ok?"测试成功："+j.translated:(j.error||"测试失败")};$("open_overlay").onclick=()=>{if(cfg&&cfg.session_id)window.open("/?session="+encodeURIComponent(cfg.session_id)+"&noavatar","_blank")};load();
</script></body></html>`

func main() {
	app := &App{cfg: loadConfig(), cache: make(map[string]string), clients: make(map[*websocket.Conn]struct{})}
	app.run()
}
