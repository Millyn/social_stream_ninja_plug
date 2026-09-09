# Windows Go 版 Social Stream Ninja + DeepSeek 翻译 Overlay

当前推荐使用 Go 版本：`main.go`。

## 编译 Windows EXE

在已安装 Go 1.22 或更高版本的环境中运行：

```bash
go mod tidy
go build -trimpath -ldflags="-s -w" -o socialstream-overlay.exe .
```

交叉编译 Windows x64：

```bash
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o socialstream-overlay.exe .
```

当前目录已经生成了 `socialstream-overlay.exe`。

## 使用

1. 双击 `socialstream-overlay.exe`。
2. 打开 `http://127.0.0.1:3000/settings`。
3. 填写 Social Stream Ninja 的 Session ID。
4. 填写 DeepSeek API Key。
5. 点击“保存设置”。
6. 点击“测试 DeepSeek”确认 API 可用。
7. 把页面显示的 Overlay URL 添加到 OBS Browser Source。

默认 Overlay 地址格式：

```text
http://127.0.0.1:3000/?session=你的SESSION_ID&noavatar
```

配置页面支持：

- Session ID
- DeepSeek API Key
- DeepSeek 模型
- 翻译超时时间
- 原文显示/隐藏
- 头像显示/隐藏
- 最大聊天消息数量
- 自动连接 Social Stream Ninja

## 配置文件和安全

配置保存在 Windows 用户配置目录：

```text
%AppData%\SocialStreamDeepSeekOverlay\config.json
%AppData%\SocialStreamDeepSeekOverlay\deepseek.key
```

API Key 单独保存于 `deepseek.key`，不会通过 `/api/config` 返回，也不会注入 Overlay 页面。

## Social Stream Ninja

在 Social Stream Ninja 的全局设置 → Mechanics 中开启：

1. `Enable remote API control of extension`
2. `Send chat messages to API server`

Go 程序会自动连接：

```text
wss://io.socialstream.ninja:443
```

并监听 channel 4 的聊天消息。

## 注意

- 程序默认监听 `0.0.0.0:3000`，允许同一局域网设备访问。
- 在 Windows 防火墙弹窗中选择允许专用网络访问；不要在公共网络开放。
- 修改 Session ID 或自动连接设置后，建议重启程序。
- 中文消息不会调用 DeepSeek；英文消息才会翻译。
- 编译出来的 EXE 可以直接复制到 Windows 使用，不需要安装 Node.js。
