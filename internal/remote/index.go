package remote

// indexHTML is a self-contained dark chat UI. two %s: title element, header.
const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1">
<title>%s</title>
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body { margin:0; font:16px/1.5 -apple-system,system-ui,sans-serif;
         background:#0d0f12; color:#e6e6e6; height:100dvh; display:flex; flex-direction:column; }
  header { padding:12px 16px; border-bottom:1px solid #23262b; font-weight:600; }
  #log { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:10px; }
  .msg { max-width:85%%; padding:10px 12px; border-radius:12px; white-space:pre-wrap; word-wrap:break-word; }
  .user { align-self:flex-end; background:#2a4d6e; }
  .assistant { align-self:flex-start; background:#1b1e23; border:1px solid #2a2e34; }
  form { display:flex; gap:8px; padding:12px; border-top:1px solid #23262b; }
  textarea { flex:1; resize:none; background:#15181d; color:#e6e6e6; border:1px solid #2a2e34;
             border-radius:10px; padding:10px; font:inherit; }
  button { background:#2a4d6e; color:#fff; border:0; border-radius:10px; padding:0 18px; font:inherit; cursor:pointer; }
  button:disabled { opacity:.5; cursor:default; }
</style>
</head>
<body>
<header id="title">%s</header>
<div id="log"></div>
<form id="form">
  <textarea id="text" rows="1" placeholder="message…" autocomplete="off"></textarea>
  <button id="send" type="submit">Send</button>
</form>
<script>
(function(){
  var log = document.getElementById('log');
  var form = document.getElementById('form');
  var text = document.getElementById('text');
  var send = document.getElementById('send');
  var cur = null;

  function atBottom(){ return log.scrollHeight - log.scrollTop - log.clientHeight < 40; }
  function scroll(){ log.scrollTop = log.scrollHeight; }

  function bubble(role, txt){
    var d = document.createElement('div');
    d.className = 'msg ' + role;
    d.textContent = txt;
    log.appendChild(d);
    return d;
  }

  var es = new EventSource('events');
  es.addEventListener('message', function(e){
    var m = JSON.parse(e.data);
    var stick = atBottom();
    if (m.role === 'assistant' && cur){ cur.textContent = m.text; cur = null; }
    else bubble(m.role, m.text);
    if (stick) scroll();
  });
  es.addEventListener('delta', function(e){
    var chunk = JSON.parse(e.data);
    var stick = atBottom();
    if (!cur) cur = bubble('assistant', '');
    cur.textContent += chunk;
    if (stick) scroll();
  });
  es.addEventListener('done', function(){ cur = null; });
  es.addEventListener('busy', function(e){ send.disabled = JSON.parse(e.data); });

  form.addEventListener('submit', function(ev){
    ev.preventDefault();
    var t = text.value.trim();
    if (!t) return;
    text.value = '';
    cur = null;
    fetch('send', {
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body: JSON.stringify({text:t})
    }).catch(function(){});
  });
  text.addEventListener('keydown', function(ev){
    if (ev.key === 'Enter' && !ev.shiftKey){ ev.preventDefault(); form.requestSubmit(); }
  });
})();
</script>
</body>
</html>`
