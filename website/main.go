package main

import (
	"embed"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed assets/*
var assetsFS embed.FS

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="bee — a minimal coding agent harness. Pure Go, single binary, zero fuss.">
<meta name="theme-color" content="#f5d656">
<title>bee</title>
<link rel="icon" href="/assets/favicon.svg">
<style>
  :root {
    --bg: #faf8f4;
    --fg: #1a1a1a;
    --muted: #6b6b6b;
    --accent: #f5d656;
    --accent-dark: #e0c040;
    --card: #ffffff;
    --code-bg: #f0ede6;
    --border: #e5e2db;
    --shadow: 0 1px 3px rgba(0,0,0,0.06);
  }
  [data-theme="dark"] {
    --bg: #1a1a1a;
    --fg: #f0ede6;
    --muted: #999;
    --accent: #f5d656;
    --accent-dark: #e0c040;
    --card: #242424;
    --code-bg: #2a2a2a;
    --border: #333;
    --shadow: 0 1px 3px rgba(0,0,0,0.3);
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    background: var(--bg);
    color: var(--fg);
    line-height: 1.6;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
    transition: background 0.3s, color 0.3s;
  }
  .container {
    max-width: 640px;
    padding: 2rem 1.5rem;
    width: 100%;
  }
  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 4rem;
    margin-top: 2rem;
  }
  .theme-toggle {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 50%;
    width: 40px;
    height: 40px;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 1.1rem;
    box-shadow: var(--shadow);
    transition: transform 0.2s;
  }
  .theme-toggle:hover { transform: scale(1.1); }
  .hero {
    text-align: center;
    margin-bottom: 4rem;
  }
  .bee {
    font-size: 4rem;
    display: block;
    margin-bottom: 1rem;
    animation: float 3s ease-in-out infinite;
  }
  @keyframes float {
    0%, 100% { transform: translateY(0); }
    50% { transform: translateY(-8px); }
  }
  h1 {
    font-size: 2.5rem;
    font-weight: 700;
    letter-spacing: -0.02em;
    margin-bottom: 0.5rem;
  }
  .tagline {
    color: var(--muted);
    font-size: 1.1rem;
    margin-bottom: 0.5rem;
  }
  .fun-fact {
    color: var(--muted);
    font-size: 0.9rem;
    font-style: italic;
    margin-top: 1rem;
  }
  .section {
    margin-bottom: 3rem;
  }
  .section h2 {
    font-size: 1.2rem;
    font-weight: 600;
    margin-bottom: 1rem;
  }
  .install-box {
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 1.5rem;
    box-shadow: var(--shadow);
  }
  .install-box code {
    display: block;
    background: var(--code-bg);
    padding: 0.75rem 1rem;
    border-radius: 8px;
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 0.85rem;
    overflow-x: auto;
    margin: 0.25rem 0;
    cursor: pointer;
    transition: background 0.2s;
  }
  .install-box code:hover {
    background: var(--accent);
    color: #1a1a1a;
  }
  .install-box code .prompt {
    color: var(--muted);
    user-select: none;
  }
  .install-box code .prompt:hover {
    color: #444;
  }
  .install-hint {
    color: var(--muted);
    font-size: 0.8rem;
    margin-top: 0.5rem;
  }
  .links {
    display: flex;
    gap: 1rem;
    flex-wrap: wrap;
  }
  .links a {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.6rem 1rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 8px;
    text-decoration: none;
    color: var(--fg);
    font-size: 0.9rem;
    box-shadow: var(--shadow);
    transition: transform 0.2s, border-color 0.2s;
  }
  .links a:hover {
    transform: translateY(-2px);
    border-color: var(--accent);
  }
  .links a .icon {
    font-size: 1.2rem;
  }
  .features {
    display: grid;
    gap: 0.75rem;
  }
  .feature {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.75rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: var(--shadow);
  }
  .feature .emoji {
    font-size: 1.3rem;
    min-width: 24px;
    text-align: center;
  }
  .feature .text {
    font-size: 0.9rem;
  }
  .feature .text strong {
    font-weight: 600;
  }
  footer {
    margin-top: auto;
    padding: 2rem 0;
    text-align: center;
    color: var(--muted);
    font-size: 0.8rem;
  }
  footer a {
    color: var(--fg);
    text-decoration: none;
    border-bottom: 1px dotted var(--muted);
  }
  footer a:hover {
    border-bottom-style: solid;
  }
  @media (max-width: 480px) {
    h1 { font-size: 2rem; }
    .bee { font-size: 3rem; }
    .container { padding: 1.5rem 1rem; }
  }
</style>
</head>
<body>
<div class="container">
  <header>
    <span style="font-weight:600;font-size:1.1rem;">🐝 bee</span>
    <button class="theme-toggle" onclick="toggleTheme()" aria-label="Toggle theme">🌙</button>
  </header>

  <main>
    <div class="hero">
      <span class="bee">🐝</span>
      <h1>bee</h1>
      <p class="tagline">a minimal coding agent harness</p>
      <p class="fun-fact">"I'm not a bot. I'm a bee. There's a difference."</p>
    </div>

    <div class="section">
      <h2>Install</h2>
      <div class="install-box">
        <code onclick="copy(this)">curl -fsSL https://raw.githubusercontent.com/elhenro/bee/main/install.sh | sh</code>
        <p class="install-hint">or: <code style="display:inline;padding:0.2rem 0.5rem;" onclick="copy(this)">go install github.com/elhenro/bee/cmd/bee@latest</code></p>
        <p class="install-hint" style="margin-top:0.5rem;">then: <code style="display:inline;padding:0.2rem 0.5rem;" onclick="copy(this)">export OPENROUTER_API_KEY=sk-... && bee</code></p>
      </div>
    </div>

    <div class="section">
      <h2>What is it?</h2>
      <div class="features">
        <div class="feature">
          <span class="emoji">🧠</span>
          <span class="text"><strong>Coding agent</strong> — writes code, runs tests, commits changes</span>
        </div>
        <div class="feature">
          <span class="emoji">⚡</span>
          <span class="text"><strong>Pure Go</strong> — single static binary, no runtime deps</span>
        </div>
        <div class="feature">
          <span class="emoji">📦</span>
          <span class="text"><strong>Skills</strong> — `+ "`" + `bee &lt;name&gt;` + "`" + ` subcommands, one binary, one PATH</span>
        </div>
        <div class="feature">
          <span class="emoji">🔬</span>
          <span class="text"><strong>Works everywhere</strong> — Ollama local to OpenRouter, tiny models to frontier</span>
        </div>
      </div>
    </div>

    <div class="section">
      <h2>Get involved</h2>
      <div class="links">
        <a href="https://github.com/elhenro/bee">
          <span class="icon">🐙</span>
          <span>GitHub</span>
        </a>
        <a href="https://pkg.go.dev/github.com/elhenro/bee">
          <span class="icon">📖</span>
          <span>Documentation</span>
        </a>
        <a href="https://github.com/elhenro/bee/releases">
          <span class="icon">🚀</span>
          <span>Releases</span>
        </a>
      </div>
    </div>
  </main>

  <footer>
    <p>built by <a href="https://github.com/elhenro">henro</a> — <span id="year"></span></p>
    <p style="margin-top:0.25rem;">"The bee is the only creature that can sting and fly at the same time."</p>
  </footer>
</div>

<script>
  document.getElementById('year').textContent = new Date().getFullYear();

  // Theme
  const toggle = document.querySelector('.theme-toggle');
  const saved = localStorage.getItem('bee-theme');
  if (saved === 'dark') document.documentElement.setAttribute('data-theme', 'dark');
  toggle.textContent = document.documentElement.getAttribute('data-theme') === 'dark' ? '☀️' : '🌙';

  function toggleTheme() {
    const current = document.documentElement.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('bee-theme', next);
    toggle.textContent = next === 'dark' ? '☀️' : '🌙';
  }

  // Copy on click
  function copy(el) {
    const text = el.textContent.replace(/^[\\s]*> /, '').trim();
    navigator.clipboard.writeText(text).then(() => {
      const orig = el.textContent;
      el.textContent = '✓ copied!';
      setTimeout(() => el.textContent = orig, 1500);
    });
  }
</script>
</body>
</html>`

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(indexHTML))
	})

	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS))))

	fmt.Printf("🐝 bee website — http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}