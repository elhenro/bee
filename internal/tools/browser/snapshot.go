package browser

import (
	"context"
	"fmt"

	"github.com/chromedp/chromedp"
)

const refAttr = "data-bee-ref"

func refSelector(ref string) string {
	return "[" + refAttr + "='" + ref + "']"
}

// snapshotJS walks the DOM, tags interactive/meaningful nodes with a stable
// ref attribute, and returns a text outline. Roles are derived from tag +
// aria-role; names from aria-label, placeholder, value, or trimmed text.
const snapshotJS = `
(function () {
  function roleOf(el) {
    var r = el.getAttribute('role');
    if (r) return r;
    var t = el.tagName.toLowerCase();
    if (t === 'a') return 'link';
    if (t === 'button') return 'button';
    if (t === 'input') return (el.type || 'text');
    if (t === 'textarea') return 'textbox';
    if (t === 'select') return 'combobox';
    if (/^h[1-6]$/.test(t)) return 'heading';
    if (t === 'nav') return 'navigation';
    return t;
  }
  function nameOf(el) {
    return (el.getAttribute('aria-label') ||
      el.getAttribute('placeholder') ||
      el.value ||
      (el.innerText || '').trim().slice(0, 80) || '').replace(/\s+/g, ' ').trim();
  }
  var sel = 'a,button,input,textarea,select,[role],h1,h2,h3,nav';
  var nodes = document.querySelectorAll(sel);
  var out = [];
  var n = 0;
  nodes.forEach(function (el) {
    var rect = el.getBoundingClientRect();
    if (rect.width === 0 && rect.height === 0) return; // skip hidden
    var ref = 'e' + (++n);
    el.setAttribute('data-bee-ref', ref);
    out.push('- ' + roleOf(el) + ' "' + nameOf(el) + '" [' + ref + ']');
  });
  return out.join('\n');
})()
`

// snapshot evaluates snapshotJS in the page and returns the text outline.
func (s *Session) snapshot(ctx context.Context) (string, error) {
	var out string
	if err := s.run(ctx, chromedp.Evaluate(snapshotJS, &out)); err != nil {
		return "", fmt.Errorf("snapshot failed: %w", err)
	}
	if out == "" {
		out = "(no interactive elements found)"
	}
	return out, nil
}
