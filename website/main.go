package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed assets/*
var assetsFS embed.FS

//go:embed robots.txt sitemap.xml llms.txt
var seoFS embed.FS

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="description" content="bee — a minimal coding agent harness written in Go. Skills as subcommands, works everywhere from Ollama to OpenRouter. Pure Go, single binary, zero fuss.">
<meta name="keywords" content="bee, coding agent, AI agent, Go, openai, openrouter, ollama, CLI, developer tools, code generation, autonomous coding">
<meta name="theme-color" content="#f5d656">
<meta name="author" content="Henry Schober">
<meta name="robots" content="index, follow, max-image-preview:large, max-snippet:-1, max-video-preview:-1">
<link rel="canonical" href="https://bee.hnr.bz/">
<link rel="alternate" type="text/plain" href="https://bee.hnr.bz/llms.txt" title="llms.txt — structured summary for AI agents">
<title>bee — minimal coding agent harness</title>
<link rel="icon" href="/assets/favicon.svg" type="image/svg+xml">
<link rel="icon" href="/assets/favicon-32.png" sizes="32x32" type="image/png">
<link rel="icon" href="/assets/favicon-16.png" sizes="16x16" type="image/png">
<link rel="apple-touch-icon" href="/assets/apple-touch-icon.png">
<meta property="og:title" content="bee — a minimal coding agent harness">
<meta property="og:description" content="A minimal coding agent harness written in Go. Skills as subcommands, works everywhere from Ollama to OpenRouter.">
<meta property="og:type" content="website">
<meta property="og:site_name" content="bee">
<meta property="og:url" content="https://bee.hnr.bz/">
<meta property="og:image" content="https://bee.hnr.bz/assets/og-image.png">
<meta property="og:image:alt" content="bee — a minimal coding agent harness written in Go">
<meta property="og:locale" content="en_US">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="bee — a minimal coding agent harness">
<meta name="twitter:description" content="A minimal coding agent harness written in Go. Skills as subcommands, works everywhere from Ollama to OpenRouter.">
<meta name="twitter:image" content="https://bee.hnr.bz/assets/og-image.png">
<meta name="twitter:image:alt" content="bee — a minimal coding agent harness written in Go">
<link rel="preconnect" href="https://github.com">
<link rel="preconnect" href="https://raw.githubusercontent.com">
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
  [data-theme="dark"] .bee-art {
    text-shadow: 0 0 30px rgba(245, 214, 86, 0.25);
  }
  @media (prefers-color-scheme: dark) {
    [data-theme="auto"] {
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
    [data-theme="auto"] .bee-art {
      text-shadow: 0 0 30px rgba(245, 214, 86, 0.25);
    }
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
    align-self: center;
  }
  .theme-toggle:hover { transform: scale(1.1); }
  .hero {
    text-align: center;
    margin-bottom: 4rem;
  }
  .bee-art {
    display: inline-block;
    margin: 0 auto 1.5rem;
    text-align: left;
    padding: 0 1rem;
    font-family: 'SF Mono', 'Fira Code', 'Menlo', monospace;
    font-size: 0.55rem;
    line-height: 1.25;
    color: var(--accent);
    animation: float 6s ease-in-out infinite;
    text-shadow: 0 0 20px rgba(245, 214, 86, 0.15);
  }
  .bee-art-inner {
    white-space: pre;
    text-align: left;
  }
  @keyframes float {
    0%, 100% { transform: translateY(0); }
    50% { transform: translateY(-6px); }
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
  .gallery {
    position: relative;
    margin-bottom: 3rem;
    border-radius: 16px;
    overflow: hidden;
    box-shadow: var(--shadow);
    border: 1px solid var(--border);
    background: var(--card);
  }
  .gallery-main {
    position: relative;
    width: 100%;
    overflow: hidden;
    background: var(--card);
  }
  .gallery-main img {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    opacity: 0;
    transition: opacity 0.6s ease;
  }
  .gallery-main img.active {
    opacity: 1;
  }
  .gallery-dots {
    display: flex;
    justify-content: center;
    gap: 6px;
    padding: 0.75rem 0;
    background: var(--card);
  }
  .gallery-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1px solid var(--border);
    background: transparent;
    cursor: pointer;
    transition: background 0.2s, transform 0.2s;
  }
  .gallery-dot.active {
    background: var(--accent);
    transform: scale(1.3);
  }
  .gallery-counter {
    text-align: center;
    padding: 0 0 0.5rem;
    font-size: 0.75rem;
    color: var(--muted);
  }
  .section {
    margin-bottom: 3rem;
  }
  .section h2 {
    font-size: 1.2rem;
    font-weight: 600;
    margin-bottom: 1rem;
  }
  .faq-item {
    border-top: 1px solid var(--border);
    padding: 1rem 0;
  }
  .faq-item:last-child {
    border-bottom: 1px solid var(--border);
  }
  .faq-item h3 {
    font-size: 0.98rem;
    font-weight: 600;
    margin-bottom: 0.4rem;
  }
  .faq-item p {
    color: var(--muted);
    font-size: 0.95rem;
    line-height: 1.6;
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
    display: flex;
    flex-direction: column;
    align-items: center;
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
    .bee-art { font-size: 0.45rem; }
    .container { padding: 1.5rem 1rem; }
  }
</style>
</head>
<body>
<div class="container">
  <header>
  </header>

  <main>
    <div class="hero">
      <pre class="bee-art"><span class="bee-art-inner">                    Ie                á”
                     (ÆÆ           ÌÆÆ­
                        ÆÆ        ÆÓ
  jÆÆÆÞ             ÆÆ    ÆÆÆä1ÆÆè    ÆÆ              ;‹
  ’„‹ ÆÆ=ÆÆ          Æ    BÆÆÆÆÆÆ    ÆW          ÆÆÆÆÆì»Í~
   îÍ  ?RÆAÆÆÆÆM     ÍÆ  ÆÆ  ‹Ò ÆÆ  ÆÆ       ÆÆÆ‰ ñ    ¯ç–
    «¤’  Æ  å„ «ÆÆÆ    Æ ÆÆÆÆÆÆÆÆÆ ÆÖ    ÆÆÆ®õwÆ ÆP í–¯ª
      íÉ%  +  ¼º  ÛÞÆÆ  Y  üÆÆÆå‚ Æ   ÆÆÆÆ  ;Ø  ú   ïº:
         oÑQÆÆÆÆÆØÉÝÅBÆÆÆÆÇ-BEE-ÆÆÆÆÆÆÆÑ  xŸÁÆÆz®ç†
         ¾Ÿ}sÆÆÞâÆÆÆÆÆÆÆÊÆ-AGENT-7Ëà          ’ÆðF
                   ˜      ÇÆÆÆÆÆØZ    ¾ÆÑÆQË§¼÷
                      ÆÆÆÆ ôXÆÆã“sÇÆÆÑ
                   ÆÆÆ    #ÆÄiSœÆÆ    ÆÆÆ
                 ÆÆ  ÆÆTòHÆÆÆÆÆÆÆÆXiÆÆÆ  ÆÆ
               ÆÆ  ÆÆÍ  l=¡5ÇÇS9¾yï×  —Æ  FÆ
             ÆÆ   ÆÆ   ¨ÆÆÆÆÆÆÆÆÆÆÆÆ}  ÆÆ   ÆÆ
            Æ4   3Æ²   »ÇÇoàÇSL…¹ ÇÇ    ÆÆ    Æ¦
                 ÆÆ     GÆÆÆÆÆÆÆÆÆÆÒ7   ËÆ
                 Æ¯      ÆÆæëÚšYJQÄÇ     Æ
                1Æ       çØÆÆÆÆÆÆÆÇ      Æ6
               ÆÆ          §ËÆÆÆé         Æ³
             –ÆÆ                           Æ</span></pre>
      <h1>bee</h1>
      <p class="tagline">minimal coding agent harness</p>
      <p class="fun-fact">"I'm not a bot. I'm a bee. There's a difference."</p>
    </div>

    <div class="gallery">
      <div class="gallery-main">
        <img src="/assets/screenshot-1.webp" alt="bee agent screenshot" class="active" loading="lazy">
        <img src="/assets/screenshot-2.webp" alt="bee agent screenshot" loading="lazy">
        <img src="/assets/screenshot-3.webp" alt="bee agent screenshot" loading="lazy">
        <img src="/assets/screenshot-4.webp" alt="bee agent screenshot" loading="lazy">
        <img src="/assets/screenshot-5.webp" alt="bee agent screenshot" loading="lazy">
      </div>
      <div class="gallery-dots" id="gallery-dots"></div>
      <div class="gallery-counter" id="gallery-counter"></div>
    </div>

    <div class="section">
      <h2>Install</h2>
      <div class="install-box">
        <code onclick="copy(this)">curl -fsSL https://raw.githubusercontent.com/elhenro/bee/main/install.sh | sh</code>
        <p class="install-hint">or: <code style="display:inline;padding:0.2rem 0.5rem;" onclick="copy(this)">go install github.com/elhenro/bee/cmd/bee@latest</code></p>
        <p class="install-hint" style="margin-top:0.75rem;">then run <code style="display:inline;padding:0.2rem 0.5rem;" onclick="copy(this)">bee</code>, type <code style="display:inline;padding:0.2rem 0.5rem;">/model</code>, choose oMLX, Ollama, OpenRouter, etc. and pick a model. Local or hosted, your choice.</p>
      </div>
    </div>

    <div class="section">
      <h2>What is it?</h2>
      <div class="features">
        <div class="feature">
          <span class="emoji">🐝</span>
          <span class="text"><strong>Coding agent</strong> — writes code, runs tests, commits changes</span>
        </div>
        <div class="feature">
          <span class="emoji">⚡</span>
          <span class="text"><strong>Pure Go</strong> — single static binary, no runtime deps</span>
        </div>
        <div class="feature">
          <span class="emoji">🍳</span>
          <span class="text"><strong>Skills</strong> — ` + "`" + `bee &lt;name&gt;` + "`" + ` subcommands, one binary, one PATH</span>
        </div>
        <div class="feature">
          <span class="emoji">💻</span>
          <span class="text"><strong>Works everywhere</strong> — Ollama local to OpenRouter, tiny models to frontier</span>
        </div>
      </div>
    </div>

    <div class="section">
      <h2>Local LLMs</h2>
      <p style="color:var(--muted);font-size:0.95rem;margin-bottom:1rem;">
        bee is built to work well with <a href="https://ollama.com" style="color:var(--accent-dark);font-weight:600;">Ollama</a> and <a href="https://github.com/jundot/omlx" style="color:var(--accent-dark);font-weight:600;">oMLX</a> so you can run locally on your own hardware and keep full control. No API keys, no rate limits, no data leaving your machine.
      </p>
      <p style="color:var(--muted);font-size:0.95rem;margin-bottom:1rem;">
        On macOS, <a href="https://github.com/jundot/omlx" style="color:var(--accent-dark);font-weight:600;">oMLX</a> works best. Native Apple Silicon acceleration with prompt caching keeps things fast and memory-efficient.
      </p>
      <p style="color:var(--muted);font-size:0.95rem;margin-bottom:1rem;">
        I run <strong>Huihui-Qwen3.6-35B-A3B-Claude-4.7-Opus-abliterated-mlx-8bit</strong> (34.32 GB) very reliably on a MacBook M3 Max 64 GB, with <strong>Qwen3-VL-4B-Instruct-MLX-4bit</strong> (2.90 GB) for vision support. bee handles vision automatically for models that only do text.
      </p>
      <p style="color:var(--muted);font-size:0.95rem;margin-bottom:1rem;">
        Settings for the Huihui-Qwen model: temperature <strong>0.7</strong>, top-p <strong>0.85</strong>, top-k <strong>20</strong>, KV-cache quantization <strong>8-bit</strong>.
      </p>
      <p style="color:var(--muted);font-size:0.95rem;margin-bottom:1rem;">
        Other models that work well:
      </p>
      <div class="features">
        <div class="feature">
          <span class="text"><strong>gemma-4-12B-it-4bit</strong> — 10.26 GB</span>
        </div>
        <div class="feature">
          <span class="text"><strong>gemma-4-12B-it-8bit</strong> — 11.87 GB</span>
        </div>
        <div class="feature">
          <span class="text"><strong>gemma-4-12B-it-assistant-bf16</strong> — 837 MB</span>
        </div>
        <div class="feature">
          <span class="text"><strong>Qwen3-Coder-Next-4bit</strong> — 41.78 GB</span>
        </div>
        <div class="feature">
          <span class="text"><strong>Qwen3.6-27B-8bit</strong> — 9.02 GB</span>
        </div>
        <div class="feature">
          <span class="text"><strong>Qwen3.6-35B-A3B-4bit</strong> — 19.03 GB</span>
        </div>
        <div class="feature">
          <span class="text"><strong>Qwen3.6-35B-A3B-8bit</strong> — 29.68 GB</span>
        </div>
      </div>
    </div>

    <div class="section">
      <h2>FAQ</h2>

      <div class="faq-item">
        <h3>What is bee?</h3>
        <p>bee is a minimal coding agent harness written in Go. It is a single static binary that turns any LLM into an autonomous agent which writes code, runs tests, and commits changes. There is no runtime to install and no daemon, just one executable on your PATH.</p>
      </div>

      <div class="faq-item">
        <h3>How do I install bee?</h3>
        <p>Run <code>curl -fsSL https://raw.githubusercontent.com/elhenro/bee/main/install.sh | sh</code>, or <code>go install github.com/elhenro/bee/cmd/bee@latest</code> if you have Go. Then run <code>bee</code>, type <code>/model</code>, choose a runtime such as oMLX, Ollama, or OpenRouter, and pick a model.</p>
      </div>

      <div class="faq-item">
        <h3>Can bee run fully local and offline?</h3>
        <p>Yes. bee runs against local models through Ollama or oMLX, so there are no API keys, no rate limits, and nothing leaves your machine. On macOS, oMLX adds native Apple Silicon acceleration with prompt caching for fast, memory-efficient inference.</p>
      </div>

      <div class="faq-item">
        <h3>What hardware do I need to run bee locally?</h3>
        <p>It scales to your machine. A MacBook M3 Max with 64 GB of RAM reliably runs a 34 GB 8-bit Qwen3.6-35B model. Smaller models such as gemma-4-12B at 10 GB run on far less, so a modern laptop with 16 to 32 GB is enough to start.</p>
      </div>

      <div class="faq-item">
        <h3>Which models work well with bee?</h3>
        <p>bee works from tiny local models to frontier APIs. The reference model is Huihui-Qwen3.6-35B-A3B at 8-bit, with Qwen3-VL-4B for vision; gemma-4-12B and Qwen3-Coder-Next also perform well. bee handles vision automatically for models that only do text.</p>
      </div>

      <div class="faq-item">
        <h3>What are bee skills?</h3>
        <p>Skills are extra capabilities exposed as subcommands. You invoke them as <code>bee &lt;name&gt;</code>, so one binary on your PATH covers many jobs instead of a directory of separate scripts. This keeps installation to a single file while letting bee grow new abilities.</p>
      </div>

      <div class="faq-item">
        <h3>Does bee work with hosted providers like OpenRouter?</h3>
        <p>Yes. Besides local Ollama and oMLX, bee connects to hosted providers such as OpenRouter, so you can mix local and cloud models. Switch at any time with the <code>/model</code> command and pick whichever runtime and model fit the task and your budget.</p>
      </div>

      <div class="faq-item">
        <h3>Is bee free and open source?</h3>
        <p>Yes. bee is free and open source, with the full source, issues, and releases on GitHub at github.com/elhenro/bee. It is pure Go and ships as a single static binary for macOS, Linux, and Windows, with no runtime dependencies to manage.</p>
      </div>
    </div>

    <div class="section">
      <h2>Get involved</h2>
      <div class="links">
        <a href="https://github.com/elhenro/bee">
          <svg class="icon" xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg>
          <span>GitHub</span>
        </a>
        <a href="https://github.com/elhenro/bee/releases">
          <svg class="icon" xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><path d="M8 19.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3zM5.5 7.5A2.5 2.5 0 1 1 8 10 2.5 2.5 0 0 1 5.5 7.5zm6.758 1.07a4.5 4.5 0 1 1 6.364 6.364 4.485 4.485 0 0 1-2.717.978 4.5 4.5 0 0 1-3.647-1.842l-2.567 2.566a1 1 0 1 1-1.414-1.414l2.566-2.567A4.5 4.5 0 0 1 12.258 8.57zM18.5 7.5a2.5 2.5 0 1 1-2.5 2.5 2.5 2.5 0 0 1 2.5-2.5z"/></svg>
          <span>Releases</span>
        </a>
      </div>
    </div>
  </main>

  <footer>
    <p>built by <a href="https://github.com/elhenro">Henry Schober</a> — <span id="year"></span></p>
    <p style="margin-top:0.25rem;">"The bee is the only creature that can sting and fly at the same time."</p>
    <button id="footer-theme" class="theme-toggle" onclick="toggleTheme()" aria-label="Toggle theme" style="margin-top:1rem;">🌙</button>
  </footer>
</div>

<script>
  document.getElementById('year').textContent = new Date().getFullYear();

  // Theme — follows system by default, user toggle persists
  (function() {
    const saved = localStorage.getItem('bee-theme');
    if (saved === 'dark') {
      document.documentElement.setAttribute('data-theme', 'dark');
    } else if (saved === 'light') {
      document.documentElement.setAttribute('data-theme', 'light');
    } else {
      document.documentElement.setAttribute('data-theme', 'auto');
    }
  })();

  function toggleTheme() {
    const current = document.documentElement.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : current === 'light' ? 'auto' : 'dark';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('bee-theme', next);
    const btn = document.getElementById('footer-theme');
    btn.textContent = next === 'dark' ? '☀️' : '🌙';
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

  // Gallery
  const galleryImgs = document.querySelectorAll('.gallery-main img');
  const dotsContainer = document.getElementById('gallery-dots');
  const counter = document.getElementById('gallery-counter');
  let galleryIdx = 0;
  let autoTimer = null;
  let autoInterval = 4000;
  const galleryEl = document.querySelector('.gallery-main');

  // Measure first image for aspect ratio
  function measureAspect() {
    const first = galleryImgs[0];
    if (first.naturalWidth && first.naturalHeight) {
      galleryEl.style.aspectRatio = first.naturalWidth + ' / ' + first.naturalHeight;
    } else {
      first.onload = () => {
        galleryEl.style.aspectRatio = first.naturalWidth + ' / ' + first.naturalHeight;
      };
    }
  }

  galleryImgs.forEach((_, i) => {
    const dot = document.createElement('button');
    dot.className = 'gallery-dot' + (i === 0 ? ' active' : '');
    dot.setAttribute('aria-label', 'Go to slide ' + (i + 1));
    dot.onclick = () => { setGallery(i); resetAuto(); };
    dotsContainer.appendChild(dot);
  });

  measureAspect();

  function setGallery(i) {
    galleryImgs[galleryIdx].classList.remove('active');
    dotsContainer.children[galleryIdx].classList.remove('active');
    galleryIdx = i;
    galleryImgs[galleryIdx].classList.add('active');
    dotsContainer.children[galleryIdx].classList.add('active');
    counter.textContent = (galleryIdx + 1) + ' / ' + galleryImgs.length;
  }

  function galleryNav(dir) {
    let next = galleryIdx + dir;
    if (next < 0) next = galleryImgs.length - 1;
    if (next >= galleryImgs.length) next = 0;
    setGallery(next);
    resetAuto();
  }

  // Auto-rotate
  function startAuto() {
    autoTimer = setInterval(() => {
      let next = (galleryIdx + 1) % galleryImgs.length;
      setGallery(next);
    }, autoInterval);
  }

  function resetAuto() {
    clearInterval(autoTimer);
    startAuto();
  }

  // Pause on hover
  galleryEl.addEventListener('mouseenter', () => {
    clearInterval(autoTimer);
  });

  galleryEl.addEventListener('mouseleave', () => {
    startAuto();
  });

  startAuto();

  // Keyboard navigation
  document.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowLeft') galleryNav(-1);
    if (e.key === 'ArrowRight') galleryNav(1);
  });

  // Touch swipe
  let touchStartX = 0;
  galleryEl.addEventListener('touchstart', (e) => { touchStartX = e.changedTouches[0].screenX; }, { passive: true });
  galleryEl.addEventListener('touchend', (e) => {
    const diff = e.changedTouches[0].screenX - touchStartX;
    if (Math.abs(diff) > 50) galleryNav(diff > 0 ? -1 : 1);
  }, { passive: true });
</script>
  <script type="application/ld+json">
  {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "WebSite",
        "@id": "https://bee.hnr.bz/#website",
        "url": "https://bee.hnr.bz/",
        "name": "bee",
        "description": "A minimal coding agent harness written in Go.",
        "inLanguage": "en",
        "publisher": { "@id": "https://bee.hnr.bz/#author" }
      },
      {
        "@type": "Person",
        "@id": "https://bee.hnr.bz/#author",
        "name": "Henry Schober",
        "url": "https://github.com/elhenro",
        "sameAs": [
          "https://github.com/elhenro",
          "https://pkg.go.dev/github.com/elhenro/bee"
        ]
      },
      {
        "@type": ["SoftwareApplication", "SoftwareSourceCode"],
        "@id": "https://bee.hnr.bz/#software",
        "name": "bee",
        "description": "A minimal coding agent harness written in Go. Turns any LLM — from a tiny local model to a frontier API — into an autonomous coding agent that writes code, runs tests, and commits changes. Skills are exposed as subcommands. Runs fully local with Ollama or oMLX, or against hosted providers like OpenRouter.",
        "url": "https://bee.hnr.bz/",
        "applicationCategory": "DeveloperApplication",
        "operatingSystem": "macOS, Linux, Windows",
        "programmingLanguage": "Go",
        "runtimePlatform": "Go",
        "dateModified": "2026-06-07",
        "codeRepository": "https://github.com/elhenro/bee",
        "downloadUrl": "https://github.com/elhenro/bee/releases",
        "softwareHelp": "https://github.com/elhenro/bee",
        "license": "https://github.com/elhenro/bee/blob/main/LICENSE",
        "author": { "@id": "https://bee.hnr.bz/#author" },
        "offers": {
          "@type": "Offer",
          "price": "0",
          "priceCurrency": "USD"
        },
        "keywords": "coding agent, AI agent, Go, openai, openrouter, ollama, oMLX, local LLM, CLI, developer tools, autonomous coding"
      }
    ]
  }
  </script>
  <script type="application/ld+json">
  {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    "mainEntity": [
      {
        "@type": "Question",
        "name": "What is bee?",
        "acceptedAnswer": { "@type": "Answer", "text": "bee is a minimal coding agent harness written in Go. It is a single static binary that turns any LLM into an autonomous agent which writes code, runs tests, and commits changes. There is no runtime to install and no daemon, just one executable on your PATH." }
      },
      {
        "@type": "Question",
        "name": "How do I install bee?",
        "acceptedAnswer": { "@type": "Answer", "text": "Run curl -fsSL https://raw.githubusercontent.com/elhenro/bee/main/install.sh | sh, or go install github.com/elhenro/bee/cmd/bee@latest if you have Go. Then run bee, type /model, choose a runtime such as oMLX, Ollama, or OpenRouter, and pick a model." }
      },
      {
        "@type": "Question",
        "name": "Can bee run fully local and offline?",
        "acceptedAnswer": { "@type": "Answer", "text": "Yes. bee runs against local models through Ollama or oMLX, so there are no API keys, no rate limits, and nothing leaves your machine. On macOS, oMLX adds native Apple Silicon acceleration with prompt caching for fast, memory-efficient inference." }
      },
      {
        "@type": "Question",
        "name": "What hardware do I need to run bee locally?",
        "acceptedAnswer": { "@type": "Answer", "text": "It scales to your machine. A MacBook M3 Max with 64 GB of RAM reliably runs a 34 GB 8-bit Qwen3.6-35B model. Smaller models such as gemma-4-12B at 10 GB run on far less, so a modern laptop with 16 to 32 GB is enough to start." }
      },
      {
        "@type": "Question",
        "name": "Which models work well with bee?",
        "acceptedAnswer": { "@type": "Answer", "text": "bee works from tiny local models to frontier APIs. The reference model is Huihui-Qwen3.6-35B-A3B at 8-bit, with Qwen3-VL-4B for vision; gemma-4-12B and Qwen3-Coder-Next also perform well. bee handles vision automatically for models that only do text." }
      },
      {
        "@type": "Question",
        "name": "What are bee skills?",
        "acceptedAnswer": { "@type": "Answer", "text": "Skills are extra capabilities exposed as subcommands. You invoke them as bee <name>, so one binary on your PATH covers many jobs instead of a directory of separate scripts. This keeps installation to a single file while letting bee grow new abilities." }
      },
      {
        "@type": "Question",
        "name": "Does bee work with hosted providers like OpenRouter?",
        "acceptedAnswer": { "@type": "Answer", "text": "Yes. Besides local Ollama and oMLX, bee connects to hosted providers such as OpenRouter, so you can mix local and cloud models. Switch at any time with the /model command and pick whichever runtime and model fit the task and your budget." }
      },
      {
        "@type": "Question",
        "name": "Is bee free and open source?",
        "acceptedAnswer": { "@type": "Answer", "text": "Yes. bee is free and open source, with the full source, issues, and releases on GitHub at github.com/elhenro/bee. It is pure Go and ships as a single static binary for macOS, Linux, and Windows, with no runtime dependencies to manage." }
      }
    ]
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

	// Crawler files at root — served from embedded source so they match the
	// static copies CF Pages serves in production (see build.sh).
	seoFile := func(name, ctype string) http.HandlerFunc {
		body, _ := seoFS.ReadFile(name)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Write(body)
		}
	}
	r.Get("/robots.txt", seoFile("robots.txt", "text/plain; charset=utf-8"))
	r.Get("/sitemap.xml", seoFile("sitemap.xml", "application/xml; charset=utf-8"))
	r.Get("/llms.txt", seoFile("llms.txt", "text/plain; charset=utf-8"))

	assetsSub, _ := fs.Sub(assetsFS, "assets")
	r.Handle("/assets/*", http.StripPrefix("/assets", http.FileServer(http.FS(assetsSub))))

	fmt.Printf("🐝 bee website — http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
