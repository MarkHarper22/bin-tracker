// Shared helpers used by every screen: API calls, safe HTML templates,
// toasts, dialogs, device naming, and scan handling.

// ---------- HTML templating (auto-escapes values) ----------

class Raw {
  constructor(s) { this.s = s; }
  toString() { return this.s; }
}

export const raw = (s) => new Raw(s);

export function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function part(v) {
  if (v instanceof Raw) return v.s;
  if (Array.isArray(v)) return v.map(part).join('');
  if (v === null || v === undefined || v === false) return '';
  return esc(v);
}

export function html(strings, ...values) {
  let out = '';
  strings.forEach((s, i) => { out += s + (i < values.length ? part(values[i]) : ''); });
  return new Raw(out);
}

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

export const icons = {
  plus: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14"/></svg>'),
  print: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 9V3h12v6"/><rect x="3" y="9" width="18" height="8" rx="2"/><path d="M6 14h12v7H6z"/></svg>'),
  trash: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>'),
  back: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M15 18l-6-6 6-6"/></svg>'),
  pin: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 21s-7-6.2-7-12a7 7 0 0 1 14 0c0 5.8-7 12-7 12z"/><circle cx="12" cy="9" r="2.5"/></svg>'),
  camera: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8h3l2-3h6l2 3h3v11H4z"/><circle cx="12" cy="13" r="3.5"/></svg>'),
  scan: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 8V5a1 1 0 0 1 1-1h3M16 4h3a1 1 0 0 1 1 1v3M20 16v3a1 1 0 0 1-1 1h-3M8 20H5a1 1 0 0 1-1-1v-3"/><path d="M7 12h10"/></svg>'),
  box: raw('<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7l9-4 9 4-9 4-9-4z"/><path d="M3 7v10l9 4 9-4V7"/><path d="M12 11v10"/></svg>'),
};

// ---------- API ----------

export async function api(path, { method = 'GET', body, blob } = {}) {
  const headers = { 'X-BinTracker': '1', 'X-Device-Name': encodeURIComponent(deviceName()) };
  let payload;
  if (blob) {
    payload = blob;
    headers['Content-Type'] = blob.type || 'application/octet-stream';
  } else if (body !== undefined) {
    payload = JSON.stringify(body);
    headers['Content-Type'] = 'application/json';
  }
  let res;
  try {
    res = await fetch('/api' + path, { method, headers, body: payload });
  } catch {
    throw new Error("Can't reach Bin Tracker. Check that it's still running on the computer and you're on the same Wi-Fi.");
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || `Request failed (${res.status})`);
    err.status = res.status;
    throw err;
  }
  return data;
}

// ---------- small utilities ----------

export const binHref = (code) => '#/bin/' + encodeURIComponent(code);
export const codeImg = (type, data) => `/api/code.svg?type=${type}&data=${encodeURIComponent(data)}`;
export const go = (hash) => { location.hash = hash; };

// The link phones should open. In Docker the server can't see the host's
// address, so fall back to however this browser reached the server.
export function phoneLink(info) {
  if (info?.phoneUrls?.length) return info.phoneUrls[0];
  if (!info?.serverMode) return null;
  if (location.protocol === 'https:') return location.origin;
  if (['localhost', '127.0.0.1', '[::1]'].includes(location.hostname)) return null;
  return `https://${location.hostname}:${info.httpsPort}`;
}

export function when(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' });
}

export function debounce(fn, ms) {
  let t;
  return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
}

export function setTitle(title) {
  document.title = title ? `${title} · Bin Tracker` : 'Bin Tracker';
}

export const field = (form, name) => form.elements.namedItem(name);

export function plural(n, one, many = one + 's') {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

// ---------- toasts ----------

export function toast(message, type = 'ok') {
  const host = document.getElementById('toasts');
  const el = document.createElement('div');
  el.className = 'toast' + (type === 'error' ? ' error' : '');
  el.textContent = message;
  host.append(el);
  setTimeout(() => el.remove(), type === 'error' ? 6000 : 2800);
}

// ---------- dialogs ----------

// openDialog shows a modal form. onConfirm(form) may be async; returning
// false keeps the dialog open, throwing shows the error. Resolves to the
// onConfirm result (or true), or null if cancelled.
export function openDialog({ title, body = '', confirmText = 'OK', cancelText = 'Cancel', danger = false, onConfirm }) {
  return new Promise((resolve) => {
    const dlg = document.createElement('dialog');
    dlg.className = 'modal';
    dlg.innerHTML = html`
      <form>
        <div class="modal-body">
          <h2>${title}</h2>
          ${body}
        </div>
        <div class="modal-actions">
          ${cancelText ? html`<button class="btn" type="button" data-cancel>${cancelText}</button>` : ''}
          <button class="btn ${danger ? 'danger' : 'primary'}" type="submit">${confirmText}</button>
        </div>
      </form>`;
    document.body.append(dlg);
    const form = $('form', dlg);
    let result = null;

    $('[data-cancel]', dlg)?.addEventListener('click', () => dlg.close());
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      const btn = $('[type=submit]', form);
      btn.disabled = true;
      try {
        const r = onConfirm ? await onConfirm(form) : true;
        if (r === false) {
          btn.disabled = false;
          return;
        }
        result = r ?? true;
        dlg.close();
      } catch (err) {
        toast(err.message, 'error');
        btn.disabled = false;
      }
    });
    dlg.addEventListener('close', () => { dlg.remove(); resolve(result); });
    dlg.showModal();
    const first = $('input:not([type=hidden]), textarea, select', dlg);
    if (first) { first.focus(); first.select?.(); }
  });
}

export async function confirmDialog(title, message, confirmText = 'OK', danger = false) {
  return !!(await openDialog({ title, body: html`<p>${message}</p>`, confirmText, danger }));
}

export async function createBinDialog(code = '') {
  const locations = await api('/locations').catch(() => []);
  const bin = await openDialog({
    title: code ? `New bin ${code}` : 'New bin',
    confirmText: 'Create bin',
    body: html`
      ${code ? html`<p class="muted">No bin uses this code yet. Add details to create it.</p>` : ''}
      <div class="stack">
        <label class="field">Name
          <input type="text" name="name" maxlength="200" placeholder="e.g. Holiday lights">
        </label>
        <label class="field">Location
          <input type="text" name="location" maxlength="200" list="dlg-locations" placeholder="e.g. Garage shelf 2">
        </label>
        ${code ? '' : html`
          <label class="field">Bin code
            <input type="text" name="code" maxlength="40" autocapitalize="characters" placeholder="Leave blank to use the next code">
          </label>`}
      </div>
      <datalist id="dlg-locations">${locations.map((l) => html`<option value="${l}">`)}</datalist>`,
    onConfirm: (form) => api('/bins', {
      method: 'POST',
      body: {
        code: code || field(form, 'code')?.value || '',
        name: field(form, 'name').value,
        location: field(form, 'location').value,
      },
    }),
  });
  if (bin) {
    toast(`Created ${bin.code}`);
    go(binHref(bin.code));
  }
  return bin;
}

// ---------- device name (shown in history) ----------

function guessDevice() {
  const ua = navigator.userAgent;
  if (/iPhone/.test(ua)) return 'iPhone';
  if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return 'iPad';
  if (/Android/.test(ua)) return 'Android phone';
  if (/Mac/.test(ua)) return 'Mac';
  if (/Windows/.test(ua)) return 'Windows PC';
  return 'Computer';
}

export function deviceName() {
  try { return localStorage.getItem('bt-device') || guessDevice(); } catch { return guessDevice(); }
}

export function setDeviceName(name) {
  try { localStorage.setItem('bt-device', name.trim() || guessDevice()); } catch { /* storage blocked */ }
}

export async function askDeviceNameOnce() {
  let saved = null;
  try { saved = localStorage.getItem('bt-device'); } catch { return; }
  if (saved) return;
  const name = await openDialog({
    title: 'Who is using this device?',
    confirmText: 'Save',
    cancelText: 'Skip',
    body: html`
      <p class="muted">Enter your name or a name for this device. The history log uses it to show who moved or changed a bin. No password needed.</p>
      <label class="field">Name
        <input type="text" name="device" maxlength="40" value="${guessDevice()}">
      </label>`,
    onConfirm: (form) => field(form, 'device').value,
  });
  setDeviceName(typeof name === 'string' ? name : guessDevice());
}

// ---------- scanning ----------

// Pulls a bin code out of scanned text. Accepts plain codes, or links that
// contain #/bin/CODE.
export function extractCode(text) {
  let t = String(text ?? '').trim();
  const m = t.match(/#\/bin\/([^?#\s]+)/);
  if (m) t = decodeURIComponent(m[1]);
  return t.toUpperCase();
}

let scanHandler = null;

// A screen can take over scans (e.g. "move" mode). Cleared on navigation.
export function setScanHandler(fn) { scanHandler = fn; }

export function handleScan(text) {
  const code = extractCode(text);
  if (!code) return;
  if (scanHandler) scanHandler(code);
  else openCode(code);
}

// Opens a bin by code, or offers to create it if the code is new.
export async function openCode(code) {
  try {
    await api('/bins/' + encodeURIComponent(code));
    go(binHref(code));
  } catch (err) {
    if (err.status === 404) {
      await createBinDialog(code);
    } else if (err.status === 400) {
      toast(`"${code}" isn't a valid bin code.`, 'error');
    } else {
      toast(err.message, 'error');
    }
  }
}

// Handheld scanners act like keyboards that type very fast and press Enter.
// Catch those bursts anywhere a text field isn't focused.
export function listenForHandheldScanner() {
  let buffer = '';
  let last = 0;
  document.addEventListener('keydown', (e) => {
    const t = e.target;
    const typing = t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement || t instanceof HTMLSelectElement || t.isContentEditable;
    if (typing || e.metaKey || e.ctrlKey || e.altKey || document.querySelector('dialog[open]')) return;
    const now = performance.now();
    if (now - last > 80) buffer = '';
    last = now;
    if (e.key === 'Enter') {
      if (buffer.length >= 3) {
        e.preventDefault();
        handleScan(buffer);
      }
      buffer = '';
    } else if (e.key.length === 1) {
      buffer += e.key;
    }
  });
}

// ---------- shared rendering ----------

export function historyList(entries, { showCode = true } = {}) {
  if (!entries.length) return html`<p class="muted">No history yet.</p>`;
  return html`<ul class="history">${entries.map((e) => html`
    <li>
      <div class="h-action">${showCode && e.binCode ? html`<a href="${binHref(e.binCode)}">${e.binCode}</a>` : ''}${e.action}</div>
      <div class="h-details muted">${e.details}</div>
      <div class="h-meta">${when(e.createdAt)}<br>${e.actor}</div>
    </li>`)}</ul>`;
}
