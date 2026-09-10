# Social Stream Ninja + DeepSeek Overlay（Go）

这是一个面向 Windows 的 Go 应用：连接 Social Stream Ninja Twitch Chat，将英文聊天发送到 DeepSeek 翻译为简体中文，并通过 OBS Browser Source 显示。

## 目录结构

```text
cmd/socialstream-overlay/      程序入口
internal/config/               配置加载、保存和校验
internal/stream/               Social Stream Ninja WebSocket、消息广播和清屏
internal/translation/          DeepSeek 翻译和缓存
internal/web/                  HTTP API、Overlay、Settings、嵌入式静态资源
```

前端资源使用 Go `embed` 编译进 EXE，不需要 Node.js、npm 或外部静态文件。

## 编译 Windows x64

```bash
gofmt -w cmd internal
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o socialstream-overlay.exe ./cmd/socialstream-overlay
```

## 使用

1. 双击 `socialstream-overlay.exe`。
2. 打开 `http://127.0.0.1:3000/settings`。
3. 填写 Social Stream Ninja Session ID 和 DeepSeek API Key。
4. 点击保存。
5. 把 settings 页面生成的地址加入 OBS Browser Source。

Overlay 直接使用：

```text
wss://io.socialstream.ninja/join/SESSION_ID/4
```

显示观看人数：

```text
http://127.0.0.1:3000/?session=SESSION_ID&viewers
```

## 一键清屏

settings 页面提供“清除当前 Overlay 所有消息”按钮。它调用：

```text
POST /api/clear
```

服务端通过本地 WebSocket Hub 广播清屏事件，所有打开的 Overlay 会立即清除当前 Chat 和事件。

## 配置和安全

Windows 配置位置：

```text
%AppData%\SocialStreamDeepSeekOverlay\config.json
%AppData%\SocialStreamDeepSeekOverlay\deepseek.key
```

DeepSeek API Key 单独保存，不会写入普通配置 JSON，也不会注入 Overlay。
