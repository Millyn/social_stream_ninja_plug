# Social Stream Ninja + DeepSeek 翻译 Overlay

这是一个本地 Overlay：接收 Social Stream Ninja API 的聊天消息，中文和非英文内容原样显示，英文聊天通过服务端调用 DeepSeek 翻译成简体中文。

## 安装

```bash
npm install
cp .env.example .env
```

编辑 `.env`，填入 DeepSeek API Key：

```env
DEEPSEEK_API_KEY=sk-xxxxxxxx
DEEPSEEK_MODEL=deepseek-chat
PORT=3000
```

不要把 API Key 放进前端文件，也不要提交 `.env`。

## Social Stream Ninja 设置

在 Social Stream Ninja 的全局设置 → Mechanics 中开启：

1. Enable remote API control of extension
2. Send chat messages to API server

聊天监听使用 API channel 4。

## 启动

```bash
npm start
```

然后把 OBS Browser Source / 浏览器地址改为：

```text
http://localhost:3000/?session=你的SESSION_ID&noavatar&original
```

其中 `session` 使用 Social Stream Ninja 的 session ID。`original` 会同时显示英文原文和中文译文；如果只想显示中文，去掉 `original`。若要关闭翻译，可加 `translate=off`。

推荐先在浏览器测试：

```bash
curl http://localhost:3000/health
```

返回 `deepseekConfigured: true` 后再打开 OBS。

## 说明

- 翻译发生在本机 Node 服务端，DeepSeek Key 不会暴露给浏览器或 OBS。
- API 设有内存缓存，重复消息不会重复调用 DeepSeek。
- 已包含重连逻辑和最多 30 条消息的显示限制，可用 `limit=50` 调整。
- API 请求失败时保留聊天消息，并显示“翻译失败”，不会阻塞聊天 Overlay。
