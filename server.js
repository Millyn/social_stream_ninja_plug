require('dotenv').config();
const express = require('express');
const path = require('path');

const app = express();
const port = Number(process.env.PORT || 3000);
const deepseekKey = process.env.DEEPSEEK_API_KEY;
const model = process.env.DEEPSEEK_MODEL || 'deepseek-chat';
const cache = new Map();
const MAX_CACHE = 500;

app.use(express.json({ limit: '32kb' }));
app.use(express.static(path.join(__dirname, 'public')));

function containsChinese(text) {
  return /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/u.test(text);
}

function looksEnglish(text) {
  const letters = text.match(/[A-Za-z]/g) || [];
  const chinese = text.match(/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/g) || [];
  return letters.length >= 2 && letters.length >= chinese.length;
}

function cacheSet(key, value) {
  if (cache.size >= MAX_CACHE) cache.delete(cache.keys().next().value);
  cache.set(key, value);
}

app.post('/api/translate', async (req, res) => {
  const text = typeof req.body?.text === 'string' ? req.body.text.trim() : '';
  if (!text || text.length > 1000) return res.status(400).json({ error: 'Invalid text' });
  if (containsChinese(text) || !looksEnglish(text)) return res.json({ translated: text, skipped: true });
  if (!deepseekKey) return res.status(503).json({ error: 'DEEPSEEK_API_KEY is not configured' });

  const cached = cache.get(text);
  if (cached) return res.json({ translated: cached, cached: true });

  try {
    const response = await fetch('https://api.deepseek.com/chat/completions', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${deepseekKey}`,
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        model,
        temperature: 0.1,
        max_tokens: 500,
        messages: [
          {
            role: 'system',
            content: '你是直播聊天翻译器。把用户提供的英文聊天内容准确、自然地翻译成简体中文。只输出中文译文，不要解释，不要加引号。保留用户名、URL、表情符号、代码、专有名词和原始换行。'
          },
          { role: 'user', content: text }
        ]
      })
    });
    const payload = await response.json();
    if (!response.ok) return res.status(response.status).json({ error: payload.error?.message || 'DeepSeek request failed' });
    const translated = payload.choices?.[0]?.message?.content?.trim();
    if (!translated) return res.status(502).json({ error: 'DeepSeek returned an empty translation' });
    cacheSet(text, translated);
    return res.json({ translated });
  } catch (error) {
    console.error('Translation error:', error.message);
    return res.status(502).json({ error: 'Translation service unavailable' });
  }
});

app.get('/health', (_req, res) => res.json({ ok: true, deepseekConfigured: Boolean(deepseekKey) }));
app.listen(port, () => console.log(`Overlay running at http://localhost:${port}`));
