import { $, $$, html, go, askDeviceNameOnce, listenForHandheldScanner, setScanHandler } from './lib.js';
import { renderBins, renderBin } from './views/bins.js';
import { renderScan } from './views/scan.js';
import { renderLabels } from './views/labels.js';
import { renderHistory, renderSettings } from './views/admin.js';

const view = document.getElementById('view');
let cleanup = null;
let renderToken = 0;

// Hash routes: #/bins, #/bin/CODE, #/scan, #/labels, #/history, #/settings.
// Each render may return a cleanup function (e.g. to stop the camera).
async function route() {
  if (cleanup) {
    try { cleanup(); } catch { /* ignore */ }
    cleanup = null;
  }
  setScanHandler(null);

  const hash = location.hash || '#/bins';
  const [path, qs = ''] = hash.slice(1).split('?');
  const params = new URLSearchParams(qs);
  const parts = path.split('/').filter(Boolean);
  const name = parts[0] || 'bins';

  const navName = name === 'bin' ? 'bins' : name;
  $$('.nav a').forEach((a) => {
    if (a.dataset.route === navName) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  });

  const token = ++renderToken;
  const ctx = { params, isCurrent: () => token === renderToken };
  window.scrollTo(0, 0);

  try {
    let result;
    switch (name) {
      case 'bins': result = await renderBins(view, ctx); break;
      case 'bin': result = await renderBin(view, decodeURIComponent(parts.slice(1).join('/')), ctx); break;
      case 'scan': result = await renderScan(view, ctx); break;
      case 'labels': result = await renderLabels(view, ctx); break;
      case 'history': result = await renderHistory(view, ctx); break;
      case 'settings': result = await renderSettings(view, ctx); break;
      default: go('#/bins'); return;
    }
    if (typeof result === 'function') {
      if (token === renderToken) cleanup = result;
      else result();
    }
  } catch (err) {
    if (token !== renderToken) return;
    view.innerHTML = html`
      <div class="empty">
        <h2>Something went wrong</h2>
        <p>${err.message}</p>
        <a class="btn" href="#/bins">Back to bins</a>
      </div>`;
  }
}

$('#quick-find').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = e.target.elements.namedItem('q');
  go('#/bins?q=' + encodeURIComponent(input.value.trim()));
  input.blur();
});

window.addEventListener('hashchange', route);
listenForHandheldScanner();
route();
askDeviceNameOnce();
