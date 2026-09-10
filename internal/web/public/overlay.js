(() => {
  const params = new URLSearchParams(location.search);
  const output = document.querySelector('#output');
  const status = document.querySelector('#status');
  const viewer = document.querySelector('#viewer-count');
  const session = params.get('session');
  const configPromise = fetch('/api/config').then(response => response.json()).catch(() => ({}));
  let socket;
  let control;
  let reconnectTimer;
  let controlReconnectTimer;
  let showOriginal = true;
  let showAvatar = false;
  let showViewers = false;
  let maxMessages = 30;

  const rawText = value => String(value ?? '').replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/g, ' ');
  const containsChinese = value => /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/u.test(value);
  const looksEnglish = value => (value.match(/[A-Za-z]/g) || []).length >= 2 && !containsChinese(value);

  function decodeMarkup(value) {
    let decoded = rawText(value);
    for (let i = 0; i < 2; i += 1) {
      const textarea = document.createElement('textarea');
      textarea.innerHTML = decoded;
      const next = textarea.value;
      if (next === decoded) break;
      decoded = next;
    }
    return decoded;
  }

  function rgba(hex, opacity) {
    let value = String(hex || '#000').replace('#', '');
    if (value.length === 3) value = value.split('').map(char => char + char).join('');
    const number = parseInt(value.slice(0, 6), 16);
    if (Number.isNaN(number)) return 'rgba(0,0,0,0)';
    const alpha = Math.max(0, Math.min(100, Number(opacity))) / 100;
    return `rgba(${number >> 16 & 255},${number >> 8 & 255},${number & 255},${alpha})`;
  }

  function safeImageSource(source) {
    const value = String(source || '').trim();
    if (!value) return '';
    if (/^data:image\/(png|jpe?g|gif|webp|avif|svg\+xml);base64,[a-z0-9+/=_\-\s]+$/i.test(value)) return value.replace(/\s+/g, '');
    if (/^blob:/i.test(value)) return value;
    try {
      const url = new URL(value, location.href);
      return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : '';
    } catch {
      return '';
    }
  }

  function srcsetSources(srcset) {
    return String(srcset || '').split(',').map(part => part.trim().split(/\s+/)[0]).filter(Boolean);
  }

  function imageCandidates(source, srcset) {
    const sources = [...srcsetSources(srcset), source].map(safeImageSource).filter(Boolean);
    const result = [];
    const add = value => { if (value && !result.includes(value)) result.push(value); };
    sources.forEach(add);
    sources.forEach(value => {
      const match = value.match(/^(https:\/\/cdn\.7tv\.app\/emote\/[^/]+\/)/i);
      if (!match) return;
      const base = match[1];
      ['4x.webp', '3x.webp', '2x.webp', '1x.webp', '4x.avif', '3x.avif', '2x.avif', '1x.avif', '4x.gif', '3x.gif', '2x.gif', '1x.gif', '4x.png', '3x.png', '2x.png', '1x.png'].forEach(size => add(base + size));
    });
    return result;
  }

  function createEmoteImage(node) {
    const source = node.getAttribute('src') || node.getAttribute('data-src') || node.getAttribute('data-original') || node.getAttribute('data-url') || '';
    const emoteId = node.getAttribute('data-emote-id') || node.getAttribute('data-emote') || node.getAttribute('data-id') || '';
    const candidates = imageCandidates(source || (emoteId ? `https://cdn.7tv.app/emote/${emoteId}/` : ''), node.getAttribute('srcset') || node.getAttribute('data-srcset'));
    if (!candidates.length) return null;
    const image = document.createElement('img');
    image.alt = node.getAttribute('alt') || '';
    image.title = node.getAttribute('title') || image.alt;
    image.dataset.candidates = JSON.stringify(candidates);
    image.dataset.candidateIndex = '0';
    image.src = candidates[0];
    image.addEventListener('error', () => {
      const list = JSON.parse(image.dataset.candidates || '[]');
      const next = Number(image.dataset.candidateIndex || 0) + 1;
      if (next < list.length) {
        image.dataset.candidateIndex = String(next);
        image.src = list[next];
      } else {
        image.remove();
      }
    });
    const width = Number(node.getAttribute('width'));
    const height = Number(node.getAttribute('height'));
    if (Number.isFinite(width) && width > 0 && width <= 256) image.width = width;
    if (Number.isFinite(height) && height > 0 && height <= 256) image.height = height;
    return image;
  }

  function parseRich(value) {
    const template = document.createElement('template');
    template.innerHTML = decodeMarkup(value);
    const fragment = document.createDocumentFragment();
    const copy = node => {
      if (node.nodeType === Node.TEXT_NODE) return document.createTextNode(node.nodeValue || '');
      if (node.nodeType !== Node.ELEMENT_NODE) return null;
      const tag = node.tagName.toLowerCase();
      if (tag === 'br') return document.createElement('br');
      if (tag === 'img') return createEmoteImage(node);
      const wrapper = document.createDocumentFragment();
      node.childNodes.forEach(child => { const safe = copy(child); if (safe) wrapper.append(safe); });
      return wrapper;
    };
    template.content.childNodes.forEach(node => { const safe = copy(node); if (safe) fragment.append(safe); });
    return fragment;
  }

  function plainText(value) {
    const template = document.createElement('template');
    template.innerHTML = decodeMarkup(value);
    return (template.content.textContent || '').replace(/\s+/g, ' ').trim();
  }

  function renderRich(target, value) { target.replaceChildren(parseRich(value)); }

  function numberValue(value) {
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string') {
      const text = value.trim().replace(/,/g, '');
      if (text !== '' && Number.isFinite(Number(text))) return Number(text);
      const match = text.match(/-?\d+(?:\.\d+)?/);
      if (match && Number.isFinite(Number(match[0]))) return Number(match[0]);
    }
    return null;
  }
  function viewerCount(data) {
    const direct = [data.viewer_count, data.viewerCount, data.viewers, data.viewers_count, data.viewer_count_total, data.counterValue, data.totalViewers, data.viewerCountTotal];
    for (const value of direct) { const number = numberValue(value); if (number !== null) return number; }
    const candidates = [data.meta, data.metadata, data.value, data.payload];
    for (const candidate of candidates) {
      const number = numberValue(candidate); if (number !== null) return number;
      if (candidate && typeof candidate === 'object') {
        for (const key of ['viewer_count', 'viewerCount', 'viewers', 'viewers_count', 'count', 'total', 'totalViewers', 'twitch', 'youtube', 'kick', 'facebook', 'tiktok']) {
          const nested = numberValue(candidate[key]); if (nested !== null) return nested;
        }
      }
    }
    return null;
  }

  function applyStyle(style) {
    const root = document.documentElement;
    const values = [
      ['font-size', `${style.font_size || 16}px`], ['name-size', `${style.name_size || 16}px`],
      ['source-size', `${style.source_size || 11}px`], ['translation-size', `${style.translation_size || 15}px`],
      ['text-color', style.text_color || '#fff'], ['name-color', style.name_color || '#ddd'],
      ['source-color', style.source_color || '#aaa'], ['translation-color', style.translation_color || '#ffe98a'],
      ['bubble-color', rgba(style.bubble_color, style.bubble_opacity ?? 35)],
      ['border-color', rgba(style.border_color || '#000', style.border_opacity ?? 0)],
      ['border-width', `${style.border_width || 0}px`], ['border-radius', `${style.border_radius ?? 5}px`],
      ['bubble-padding', `${style.bubble_padding ?? 5}px`], ['message-gap', `${style.message_gap ?? 7}px`],
      ['viewer-color', style.viewer_count_color || '#aaa']
    ];
    values.forEach(([name, value]) => root.style.setProperty(`--${name}`, value));
    if (!style.shadow) root.style.setProperty('--shadow', 'none');
  }

  function clearMessages() { output.replaceChildren(); }

  async function translate(original, target) {
    if (!looksEnglish(original)) { target.remove(); return; }
    try {
      const response = await fetch('/api/translate', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: original }) });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || '翻译失败');
      if (result.skipped || !result.translated) { target.remove(); return; }
      target.textContent = result.translated;
      target.classList.remove('pending');
    } catch (error) {
      target.textContent = '翻译失败'; target.title = error.message; target.classList.add('error');
    }
  }

  function addMessage(data) {
    if (!data || typeof data !== 'object') return;
    if (data.type === 'clear' || data.action === 'clear') { clearMessages(); return; }
    const count = viewerCount(data);
    const isViewerEvent = data.event === 'viewer_update' || data.event === 'viewer_update_total' || data.event === 'viewer_updates' || data.type === 'viewer_update' || data.type === 'viewer_update_total' || data.type === 'viewer_updates';
    if (Number.isFinite(count)) {
      if (showViewers || isViewerEvent) { viewer.textContent = `👁 ${Math.max(0, Math.round(count)).toLocaleString('zh-CN')}`; viewer.classList.add('visible'); }
      if (isViewerEvent) return;
    }
    const message = rawText(data.chatmessage);
    const messageText = plainText(message);
    const name = plainText(data.chatname || data.name);
    const event = rawText(data.event);
    if (!message && !event && !name && !data.contentimg) return;
    const row = document.createElement('article'); row.className = 'message';
    const bubble = document.createElement('div'); bubble.className = 'bubble';
    const header = document.createElement('div');
    const nameNode = document.createElement('span'); nameNode.className = 'name'; nameNode.textContent = name || 'Social Stream'; header.append(nameNode);
    if (data.type) { const source = document.createElement('span'); source.className = 'source'; source.textContent = `[${data.type}]`; header.append(source); }
    bubble.append(header);
    if (event && !messageText) { const eventNode = document.createElement('div'); eventNode.className = 'event'; renderRich(eventNode, event); bubble.append(eventNode); }
    if (message) { const original = document.createElement('span'); original.className = 'original'; renderRich(original, message); original.hidden = !showOriginal; bubble.append(original); const translation = document.createElement('span'); translation.className = 'translation pending'; translation.textContent = '翻译中…'; bubble.append(translation); translate(messageText, translation); }
    if (data.contentimg) { const mediaSrc = safeImageSource(data.contentimg); if (mediaSrc) { const media = document.createElement('img'); media.className = 'content-image'; media.src = mediaSrc; media.alt = ''; media.onerror = () => media.remove(); bubble.append(media); } }
    if (data.chatimg && showAvatar) { const avatarSrc = safeImageSource(data.chatimg); if (avatarSrc) { const avatar = document.createElement('img'); avatar.className = 'avatar'; avatar.src = avatarSrc; avatar.alt = ''; avatar.onerror = () => avatar.remove(); row.append(avatar); } }
    row.append(bubble); output.append(row);
    while (output.querySelectorAll('.message').length > maxMessages) output.querySelector('.message')?.remove();
  }

  function connectControl() {
    const protocol = location.protocol === 'https:' ? 'wss' : 'ws';
    control = new WebSocket(`${protocol}://${location.host}/ws?session=${encodeURIComponent(session || '')}`);
    control.onmessage = event => { try { const data = JSON.parse(event.data); if (data.type === 'clear' || data.action === 'clear') clearMessages(); } catch {} };
    control.onclose = () => { clearTimeout(controlReconnectTimer); controlReconnectTimer = setTimeout(connectControl, 3000); };
  }
  async function start() {
    const config = await configPromise;
    const style = config.style || {};
    showOriginal = params.has('original') ? !params.has('nooriginal') : !!config.show_original;
    showAvatar = params.has('avatar') ? !params.has('noavatar') : !!style.show_avatar;
    showViewers = params.has('noviewers') ? false : (params.has('viewers') || params.has('showviewers') || params.has('showviewercount') || !!style.show_viewer_count);
    maxMessages = Number(params.get('limit') || config.max_messages || 30);
    applyStyle(style); if (showViewers) viewer.classList.add('visible'); connectControl();
    if (!session) { status.textContent = '缺少 session 参数'; return; }
    status.textContent = '连接中…';
    socket = new WebSocket(`wss://io.socialstream.ninja/join/${encodeURIComponent(session)}/4`);
    socket.onopen = () => { status.textContent = '已连接'; };
    socket.onmessage = event => {
      try {
        let data = JSON.parse(event.data);
        for (let i = 0; i < 3 && data && typeof data === 'object'; i += 1) {
          if (typeof data.value === 'string') { try { data = JSON.parse(data.value); continue; } catch {} }
          if (data.data && typeof data.data === 'object' && !data.chatmessage && !data.event) { data = data.data; continue; }
          break;
        }
        addMessage(data);
      } catch {}
    };
    socket.onclose = () => { status.textContent = '连接断开，重连中…'; clearTimeout(reconnectTimer); reconnectTimer = setTimeout(start, 3000); };
  }

  start();
})();
