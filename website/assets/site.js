(() => {
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];
  const root = document.documentElement;

  // theme follows the system until the toggle is used, then the choice sticks
  const dark = matchMedia('(prefers-color-scheme: dark)');
  const effective = () => (root.dataset.theme === 'auto' ? (dark.matches ? 'dark' : 'light') : root.dataset.theme);
  $('#theme')?.addEventListener('click', () => {
    const next = effective() === 'dark' ? 'light' : 'dark';
    root.dataset.theme = next;
    try { localStorage.setItem('bee-theme', next); } catch {}
    $$('meta[name="theme-color"]').forEach((m) => {
      m.removeAttribute('media');
      m.content = next === 'dark' ? '#15141a' : '#fffbf2';
    });
  });

  const nav = $('.nav');
  const onScroll = () => nav.classList.toggle('scrolled', scrollY > 8);
  addEventListener('scroll', onScroll, { passive: true });
  onScroll();

  // copy buttons
  async function copy(text) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      const ta = Object.assign(document.createElement('textarea'), { value: text });
      ta.style.cssText = 'position:fixed;opacity:0';
      document.body.append(ta);
      ta.select();
      const ok = document.execCommand('copy');
      ta.remove();
      return ok;
    }
  }
  document.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-copy]');
    if (!btn) return;
    const target = $(btn.dataset.copy);
    const ok = await copy(target.textContent.trim());
    if (!ok) getSelection().selectAllChildren(target); // let the reader copy by hand
    const label = btn.textContent;
    btn.textContent = ok ? 'Copied' : 'Press ⌘C';
    btn.classList.toggle('done', ok);
    setTimeout(() => {
      btn.textContent = label;
      btn.classList.remove('done');
    }, 1400);
  });

  // install method tabs
  $$('[data-cmd]').forEach((b) =>
    b.addEventListener('click', () => {
      $('#install-cmd').textContent = b.dataset.cmd;
      $$('[data-cmd]').forEach((o) => o.setAttribute('aria-selected', o === b));
    }),
  );

  // screenshot gallery: tabs, arrow keys, swipe, full size view
  const gal = $('[data-gallery]');
  if (gal) {
    const tabs = $$('[role="tab"]', gal);
    const panels = $$('.panel', gal);
    let at = 0;
    const show = (i, focus) => {
      at = (i + tabs.length) % tabs.length;
      tabs.forEach((t, n) => {
        t.setAttribute('aria-selected', n === at);
        t.tabIndex = n === at ? 0 : -1;
        panels[n].classList.toggle('active', n === at);
      });
      if (focus) tabs[at].focus();
    };
    tabs.forEach((t, i) => {
      t.addEventListener('click', () => show(i));
      t.addEventListener('keydown', (e) => {
        const to = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[e.key];
        if (to === undefined) return;
        e.preventDefault();
        show(to, true);
      });
    });
    show(0);

    let x0 = null;
    const stages = $('.panels', gal);
    stages.addEventListener('pointerdown', (e) => { x0 = e.pointerType === 'touch' ? e.clientX : null; });
    stages.addEventListener('pointerup', (e) => {
      if (x0 !== null && Math.abs(e.clientX - x0) > 50) show(at + (e.clientX < x0 ? 1 : -1));
      x0 = null;
    });

    const box = $('#lightbox');
    gal.addEventListener('click', (e) => {
      const img = e.target.closest('.window img');
      if (!img || !box?.showModal) return;
      const big = $('img', box);
      big.src = img.currentSrc || img.src;
      big.alt = img.alt;
      box.showModal();
    });
    box?.addEventListener('click', () => box.close());
  }

  // hero video: play only while visible, never for reduced motion, always pausable
  const vid = $('#hero-video');
  const tog = $('.vid-toggle');
  if (vid && tog) {
    const reduce = matchMedia('(prefers-reduced-motion: reduce)');
    let manual = false;
    vid.controls = false; // native controls stay only when scripts do not run
    const sync = () => {
      tog.classList.toggle('paused', vid.paused);
      tog.setAttribute('aria-label', vid.paused ? 'Play demo' : 'Pause demo');
    };
    tog.addEventListener('click', () => {
      manual = !vid.paused;
      vid.paused ? vid.play() : vid.pause();
    });
    vid.addEventListener('play', sync);
    vid.addEventListener('pause', sync);
    new IntersectionObserver(
      ([e]) => {
        if (!e.isIntersecting) vid.pause();
        else if (!manual && !reduce.matches) vid.play().catch(() => {});
      },
      { threshold: 0.25 },
    ).observe(vid);
    sync();
  }

  const year = $('#year');
  if (year) year.textContent = new Date().getFullYear();
})();
