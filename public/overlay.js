(() => {
  const params = new URLSearchParams(location.search);
  const session = params.get('session');
  const output = document.querySelector('#output');
  const status = document.querySelector('#status');
  const maxMessages = Number(params.get('limit') || 30);
  const translationEnabled = params.get('translate') !== 'off';
  const originalVisible = params.get('original') !== 'off';
  let socket;
  let reconnectTimer;

  if (!session) {
    status.textContent = '缺少 session 参数';
    return;
  }

  const isChinese = text => /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/u.test(text);
  const isEnglish = text => {
    const letters = (text.match(/[A-Za-z]/g) || []).length;
    return letters >= 2 && letters >= (text.match(/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/g) || []).length;
  };

  function safeText(value) {
    return String(value ?? '').replace(/[\u0000-\u001f]/g, ' ').trim();
  }

  async function translate(text, target) {
    if (!translationEnabled || !isEnglish(text) || isChinese(text)) return;
    target.textContent = '翻译中…';
    target.classList.add('pending');
    try {
      const response = await fetch('/api/translate', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text })
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || '翻译失败');
      if (result.skipped || result.translated === text) {
        target.remove();
        return;
      }
      target.textContent = result.translated;
      target.classList.remove('pending');
    } catch (error) {
      target.textContent = '翻译失败';
      target.title = error.message;
      target.classList.remove('pending');
      target.classList.add('error');
    }
  }

  function addMessage(data) {
    if (!data || typeof data !== 'object' || (!data.chatmessage && !data.chatname)) return;
    if (data.event && !data.chatmessage) return;
    const text = safeText(data.chatmessage);
    const name = safeText(data.chatname || '');
    if (!text && !name) return;

    const row = document.createElement('article');
    row.className = 'message';
    if (data.nameColor) row.style.setProperty('--name-color', data.nameColor);

    if (data.chatimg && params.has('noavatar') === false) {
      const avatar = document.createElement('img');
      avatar.className = 'avatar'; avatar.alt = ''; avatar.loading = 'lazy'; avatar.src = data.chatimg;
      avatar.onerror = () => avatar.remove();
      row.appendChild(avatar);
    }

    const bubble = document.createElement('div');
    bubble.className = 'bubble';
    const header = document.createElement('div');
    const nameNode = document.createElement('span'); nameNode.className = 'name'; nameNode.textContent = name;
    header.appendChild(nameNode);
    if (data.type) { const source = document.createElement('span'); source.className = 'source'; source.textContent = `[${data.type}]`; header.appendChild(source); }
    bubble.appendChild(header);

    if (text) {
      const original = document.createElement('span');
      original.className = 'original'; original.textContent = text;
      original.hidden = !originalVisible;
      bubble.appendChild(original);
      const translated = document.createElement('span');
      translated.className = 'translation';
      bubble.appendChild(translated);
      translate(text, translated);
    }
    row.appendChild(bubble);
    output.appendChild(row);
    while (output.querySelectorAll('.message').length > maxMessages) output.querySelector('.message')?.remove();
  }

  function connect() {
    status.textContent = '连接中…';
    socket = new WebSocket('wss://io.socialstream.ninja:443');
    socket.onopen = () => {
      socket.send(JSON.stringify({ join: session, in: 4, out: 1 }));
    };
      status.textContent = translationEnabled ? 'DeepSeek 翻译已启用' : '翻译已关闭';
    socket.onmessage = event => {
      try {
        const data = JSON.parse(event.data);
        // The server may wrap a payload in a value field.
        const payload = typeof data.value === 'string' ? (() => { try { return JSON.parse(data.value); } catch { return data; } })() : data;
        addMessage(payload);
      } catch { /* Ignore non-JSON control frames. */ }
    };
    socket.onclose = () => {
      status.textContent = '连接断开，重连中…';
      clearTimeout(reconnectTimer); reconnectTimer = setTimeout(connect, 3000);
    };
  }
  connect();
})();
