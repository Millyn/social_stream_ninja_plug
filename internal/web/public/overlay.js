(() => {
  const p = new URLSearchParams(location.search), output = document.querySelector('#output'), status = document.querySelector('#status'), viewer = document.querySelector('#viewer-count');
  const session = p.get('session'), cfgPromise = fetch('/api/config').then(r => r.json()).catch(() => ({})); let cfg = {}, socket, control, reconnect, controlReconnect;
  const rawText = x => String(x ?? '').replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/g, ' ');
  const chinese = x => /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/u.test(x);
  const english = x => (x.match(/[A-Za-z]/g) || []).length >= 2 && !chinese(x);
  const rgba = (hex, opacity) => { let h = String(hex || '#000').replace('#',''); if(h.length===3) h=h.split('').map(x=>x+x).join(''); const n=parseInt(h.slice(0,6),16); if(Number.isNaN(n)) return 'rgba(0,0,0,0)'; return `rgba(${n>>16&255},${n>>8&255},${n&255},${Math.max(0,Math.min(100,Number(opacity)))/100})`; };
  function safeImageSource(src) {
    const value = String(src || '').trim();
    if (!value) return '';
    if (/^data:image\/(png|jpe?g|gif|webp|avif|svg\+xml);base64,[a-z0-9+/=\s]+$/i.test(value)) return value.replace(/\s+/g, '');
    if (/^blob:/i.test(value)) return value;
    try { const url = new URL(value, location.href); return url.protocol === 'http:' || url.protocol === 'https:' ? url.href : ''; } catch { return ''; }
  }
  function decodeMarkup(value) {
    let decoded = rawText(value);
    for (let i = 0; i < 2; i++) {
      const entity = document.createElement('textarea'); entity.innerHTML = decoded;
      const next = entity.value; if (next === decoded) break; decoded = next;
    }
    return decoded;
  }

  // Social Stream Ninja sends emotes as HTML, commonly as <img> tags. Never
  // assign that HTML to innerHTML directly: copy only safe text, <br>, and
  // remote images so a chat message cannot inject scripts or event handlers.
  function parseRich(raw) {
    const template = document.createElement('template');
    template.innerHTML = decodeMarkup(raw);
    const fragment = document.createDocumentFragment();
    const copy = node => {
      if (node.nodeType === Node.TEXT_NODE) return document.createTextNode(node.nodeValue || '');
      if (node.nodeType !== Node.ELEMENT_NODE) return null;
      const tag = node.tagName.toLowerCase();
      if (tag === 'br') return document.createElement('br');
      if (tag === 'img') {
        const src = node.getAttribute('src') || node.getAttribute('data-src') || node.getAttribute('data-original') || node.getAttribute('data-url') || '';
        const imageSrc = safeImageSource(src);
        if (!imageSrc) return null;
        const image = document.createElement('img'); image.src = imageSrc; image.alt = node.getAttribute('alt') || ''; image.title = node.getAttribute('title') || '';
        const width = Number(node.getAttribute('width')), height = Number(node.getAttribute('height'));
        if (Number.isFinite(width) && width > 0 && width <= 256) image.width = width;
        if (Number.isFinite(height) && height > 0 && height <= 256) image.height = height;
        return image;
      }
      const wrapper = document.createDocumentFragment();
      node.childNodes.forEach(child => { const safe = copy(child); if (safe) wrapper.append(safe); });
      return wrapper;
    };
    template.content.childNodes.forEach(node => { const safe = copy(node); if (safe) fragment.append(safe); });
    return fragment;
  }
  function plainText(raw) {
    const template = document.createElement('template'); template.innerHTML = decodeMarkup(raw);
    return (template.content.textContent || '').replace(/\s+/g, ' ').trim();
  }
  function renderRich(target, raw) { target.replaceChildren(parseRich(raw)); }
  function applyStyle(s) { const root=document.documentElement; [['font-size',(s.font_size||16)+'px'],['name-size',(s.name_size||16)+'px'],['source-size',(s.source_size||11)+'px'],['translation-size',(s.translation_size||15)+'px'],['text-color',s.text_color||'#fff'],['name-color',s.name_color||'#ddd'],['source-color',s.source_color||'#aaa'],['translation-color',s.translation_color||'#ffe98a'],['bubble-color',rgba(s.bubble_color,s.bubble_opacity??35)],['border-color',rgba(s.border_color||'#000',s.border_opacity??0)],['border-width',(s.border_width||0)+'px'],['border-radius',(s.border_radius??5)+'px'],['bubble-padding',(s.bubble_padding??5)+'px'],['message-gap',(s.message_gap??7)+'px'],['viewer-color',s.viewer_count_color||'#aaa']].forEach(([k,v])=>root.style.setProperty('--'+k,v)); if(!s.shadow)root.style.setProperty('--shadow','none'); }
  function clear() { output.replaceChildren(); }
  async function translate(original, target) { if(!english(original)) { target.remove(); return; } try { const r=await fetch('/api/translate',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:original})}),j=await r.json(); if(!r.ok)throw Error(j.error||'翻译失败'); if(j.skipped||!j.translated){target.remove();return} target.textContent=j.translated;target.classList.remove('pending'); } catch(e) { target.textContent='翻译失败';target.title=e.message;target.classList.add('error'); } }
  function add(d) { if(!d||typeof d!=='object')return; if(d.type==='clear'||d.action==='clear'){clear();return} const message=rawText(d.chatmessage), name=plainText(d.chatname||d.name), event=rawText(d.event), messageText=plainText(message); if(!messageText&&!event&&!name&&!message)return; const count=Number(d.viewer_count??d.viewerCount??d.viewers??d.meta?.viewer_count??d.meta?.viewerCount); if(Number.isFinite(count)&&showViewers){viewer.textContent='👁 '+Math.max(0,Math.round(count)).toLocaleString('zh-CN');viewer.classList.add('visible');return;} const row=document.createElement('article');row.className='message';const bubble=document.createElement('div');bubble.className='bubble';const head=document.createElement('div');const nameEl=document.createElement('span');nameEl.className='name';nameEl.textContent=name||'Social Stream';head.append(nameEl);if(d.type){const src=document.createElement('span');src.className='source';src.textContent=`[${d.type}]`;head.append(src)}bubble.append(head);if(event&&!messageText){const e=document.createElement('div');e.className='event';renderRich(e,event);bubble.append(e)}if(message){const o=document.createElement('span');o.className='original';renderRich(o,message);o.hidden=!showOriginal;bubble.append(o);const t=document.createElement('span');t.className='translation pending';t.textContent='翻译中…';bubble.append(t);translate(messageText,t)}if(d.chatimg&&showAvatar){const img=document.createElement('img');img.className='avatar';img.src=d.chatimg;img.alt='';img.onerror=()=>img.remove();row.append(img)}row.append(bubble);output.append(row);while(output.querySelectorAll('.message').length>max)output.querySelector('.message')?.remove();}
  let showOriginal=true,showAvatar=false,showViewers=false,max=30;
  function connectControl(){const protocol=location.protocol==='https:'?'wss':'ws';control=new WebSocket(`${protocol}://${location.host}/ws`);control.onmessage=e=>{try{const d=JSON.parse(e.data);if(d.type==='clear'||d.action==='clear')clear()}catch{}};control.onclose=()=>{clearTimeout(controlReconnect);controlReconnect=setTimeout(connectControl,3000)};}
  async function start(){ cfg=await cfgPromise; const s=cfg.style||{}; showOriginal=p.has('original')?!p.has('nooriginal'):!!cfg.show_original;showAvatar=p.has('avatar')?!p.has('noavatar'):!!s.show_avatar;showViewers=p.has('viewers')||p.has('showviewers')||p.has('showviewercount')?!p.has('noviewers'):!!s.show_viewer_count;max=Number(p.get('limit')||cfg.max_messages||30);applyStyle(s);connectControl();if(!session){status.textContent='缺少 session 参数';return}status.textContent='连接中…';socket=new WebSocket('wss://io.socialstream.ninja/join/'+encodeURIComponent(session)+'/4');socket.onopen=()=>{status.textContent='已连接'};socket.onmessage=e=>{try{let d=JSON.parse(e.data);for(let i=0;i<3&&d&&typeof d==='object';i++){if(typeof d.value==='string'){try{d=JSON.parse(d.value);continue}catch{}}if(d.data&&typeof d.data==='object'&&!d.chatmessage&&!d.event){d=d.data;continue}break}add(d)}catch{}};socket.onclose=()=>{status.textContent='连接断开，重连中…';clearTimeout(reconnect);reconnect=setTimeout(start,3000)};}
  start();
})();
