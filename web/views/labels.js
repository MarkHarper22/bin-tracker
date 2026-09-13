import { $, $$, api, html, toast, setTitle, codeImg, debounce, plural } from '../lib.js';

const IN = 25.4; // mm per inch
const SHEETS = 'Label sheets (regular printer)';
const ROLLS = 'Label printers (Dymo, Brother, Zebra…)';

// Sheet measurements are in inches: label size, page margins, and pitch
// (distance from one label's left/top edge to the next one's).
const LAYOUTS = [
  { id: 'avery5160', group: SHEETS, name: 'Avery 5160 / 8160: 30 per sheet, 2⅝ × 1 in', kind: 'sheet', pageW: 8.5, pageH: 11, cols: 3, rows: 10, w: 2.625, h: 1, left: 0.1875, top: 0.5, pitchX: 2.75, pitchY: 1 },
  { id: 'avery5163', group: SHEETS, name: 'Avery 5163 / 8163: 10 per sheet, 4 × 2 in', kind: 'sheet', pageW: 8.5, pageH: 11, cols: 2, rows: 5, w: 4, h: 2, left: 0.15625, top: 0.5, pitchX: 4.1875, pitchY: 2 },
  { id: 'avery5164', group: SHEETS, name: 'Avery 5164 / 8164: 6 per sheet, 4 × 3⅓ in', kind: 'sheet', pageW: 8.5, pageH: 11, cols: 2, rows: 3, w: 4, h: 3.3333, left: 0.15625, top: 0.5, pitchX: 4.1875, pitchY: 3.3333 },
  { id: 'custom-sheet', group: SHEETS, name: 'Custom sheet…', kind: 'sheet', custom: true },
  { id: 'dymo30334', group: ROLLS, name: 'Dymo 30334: 2¼ × 1¼ in', kind: 'roll', w: 2.25, h: 1.25 },
  { id: 'dymo30336', group: ROLLS, name: 'Dymo 30336: 2⅛ × 1 in', kind: 'roll', w: 2.125, h: 1 },
  { id: 'dymo30252', group: ROLLS, name: 'Dymo 30252 address: 3½ × 1⅛ in', kind: 'roll', w: 3.5, h: 1.125 },
  { id: 'dk1201', group: ROLLS, name: 'Brother DK-1201: 90 × 29 mm', kind: 'roll', w: 90 / IN, h: 29 / IN },
  { id: 'dk1209', group: ROLLS, name: 'Brother DK-1209: 62 × 29 mm', kind: 'roll', w: 62 / IN, h: 29 / IN },
  { id: 'roll2x1', group: ROLLS, name: 'Thermal 2 × 1 in (Zebra, Rollo, etc.)', kind: 'roll', w: 2, h: 1 },
  { id: 'roll3x2', group: ROLLS, name: 'Thermal 3 × 2 in', kind: 'roll', w: 3, h: 2 },
  { id: 'roll4x6', group: ROLLS, name: 'Thermal 4 × 6 in (shipping label)', kind: 'roll', w: 4, h: 6 },
  { id: 'custom-roll', group: ROLLS, name: 'Custom label size…', kind: 'roll', custom: true },
];

const DEFAULTS = {
  type: 'qr',
  layout: 'avery5160',
  showName: true,
  showLocation: false,
  copies: 1,
  skip: 0,
  rotate: false,
  offsetX: 0,
  offsetY: 0,
  customRoll: { w: 2, h: 1 },
  customSheet: { paper: 'letter', cols: 3, rows: 10, w: 2.625, h: 1, left: 0.1875, top: 0.5, pitchX: 2.75, pitchY: 1 },
};

const STORAGE_KEY = 'bt-label-options';

function loadOptions() {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}');
    return {
      ...DEFAULTS,
      ...saved,
      customRoll: { ...DEFAULTS.customRoll, ...saved.customRoll },
      customSheet: { ...DEFAULTS.customSheet, ...saved.customSheet },
    };
  } catch {
    return structuredClone(DEFAULTS);
  }
}

function saveOptions(o) {
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify(o)); } catch { /* storage blocked */ }
}

const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v));
const num = (v, lo, hi, def) => { const n = parseFloat(v); return Number.isFinite(n) ? clamp(n, lo, hi) : def; };
const int = (v, lo, hi, def) => { const n = parseInt(v, 10); return Number.isFinite(n) ? clamp(n, lo, hi) : def; };
const r4 = (v) => +v.toFixed(4);

function resolveLayout(o) {
  const base = LAYOUTS.find((l) => l.id === o.layout) || LAYOUTS[0];
  if (!base.custom) return base;
  if (base.kind === 'roll') {
    return { ...base, w: num(o.customRoll.w, 0.5, 12, 2), h: num(o.customRoll.h, 0.3, 12, 1) };
  }
  const c = o.customSheet;
  const paper = c.paper === 'a4' ? { pageW: 210 / IN, pageH: 297 / IN } : { pageW: 8.5, pageH: 11 };
  return {
    ...base,
    ...paper,
    cols: int(c.cols, 1, 20, 3),
    rows: int(c.rows, 1, 40, 10),
    w: num(c.w, 0.3, 11, 2.625),
    h: num(c.h, 0.3, 11, 1),
    left: num(c.left, 0, 5, 0.1875),
    top: num(c.top, 0, 5, 0.5),
    pitchX: num(c.pitchX, 0.3, 11, 2.75),
    pitchY: num(c.pitchY, 0.3, 11, 1),
  };
}

// Builds one label's contents, sizing the code and text to fit the label.
function labelHTML(entry, L, o, position) {
  const W = L.w;
  const H = L.h;
  const pad = Math.min(0.08, Math.min(W, H) * 0.08);
  const iw = W - 2 * pad;
  const ih = H - 2 * pad;
  const code = entry.code;
  const len = Math.max(code.length, 4);
  const name = o.showName ? entry.name : '';
  const loc = o.showLocation ? entry.location : '';
  const src = codeImg(o.type, code);

  const text = (codePt, namePt, locPt, lines, extraStyle = '') => html`
    <div class="lbl-text" style="${extraStyle}">
      <div class="lbl-code" style="font-size:${r4(codePt)}pt">${code}</div>
      ${name ? html`<div class="lbl-name" style="font-size:${r4(namePt)}pt;-webkit-line-clamp:${lines}">${name}</div>` : ''}
      ${loc ? html`<div class="lbl-loc" style="font-size:${r4(locPt)}pt">${loc}</div>` : ''}
    </div>`;

  // Wide QR labels: code on the left, text on the right.
  if (o.type === 'qr' && W >= H * 1.15) {
    const qr = Math.min(ih, iw * 0.45);
    const tw = iw - qr - pad;
    const codePt = clamp(Math.min(ih * 72 * 0.26, (tw * 72) / (len * 0.62)), 4, 24);
    const namePt = clamp(codePt * 0.9, 4, 18);
    const locPt = clamp(codePt * 0.75, 4, 13);
    const room = ih * 72 - codePt * 1.15 - (loc ? locPt * 1.15 : 0);
    const lines = clamp(Math.floor(room / (namePt * 1.12)), 1, 4);
    return html`
      <div class="lbl" style="${position}padding:${r4(pad)}in;gap:${r4(pad)}in">
        <img src="${src}" alt="" style="width:${r4(qr)}in;height:${r4(qr)}in">
        ${text(codePt, namePt, locPt, lines, `width:${r4(tw)}in`)}
      </div>`;
  }

  // Barcodes and tall labels: code on top, text underneath.
  const codePt = clamp(Math.min(ih * 72 * (o.type === 'qr' ? 0.1 : 0.2), (iw * 72) / (len * 0.62)), 4, 26);
  const namePt = clamp(codePt * 0.9, 4, 20);
  const locPt = clamp(codePt * 0.75, 4, 14);
  const lines = H >= 2 ? 2 : 1;
  const textIn = (codePt * 1.15 + (name ? namePt * 1.12 * lines : 0) + (loc ? locPt * 1.15 : 0)) / 72;
  const gap = pad * 0.6;
  const avail = Math.max(0.2, ih - textIn - gap);
  const img = o.type === 'qr'
    ? html`<img src="${src}" alt="" style="width:${r4(Math.min(iw, avail))}in;height:${r4(Math.min(iw, avail))}in">`
    : html`<img src="${src}" alt="" style="width:${r4(iw)}in;height:${r4(avail)}in">`;
  return html`
    <div class="lbl stacked" style="${position}padding:${r4(pad)}in">
      ${img}
      ${text(codePt, namePt, locPt, lines, `margin-top:${r4(gap)}in`)}
    </div>`;
}

export async function renderLabels(view, ctx) {
  setTitle('Print labels');
  const opts = loadOptions();
  const preselected = new Set((ctx.params.get('bins') || '').split(',').map((c) => c.trim().toUpperCase()).filter(Boolean));

  view.innerHTML = html`
    <div class="page-head no-print"><h1>Print labels</h1></div>
    <div class="labels-layout">
      <div class="labels-controls no-print">
        <section class="card">
          <h2>1. Choose bins</h2>
          <input type="search" id="pick-q" placeholder="Filter bins…" aria-label="Filter bins" autocomplete="off">
          <div class="row" style="margin-top:8px;align-items:center">
            <button class="btn small" type="button" id="pick-all">Select shown</button>
            <button class="btn small" type="button" id="pick-none">Clear</button>
            <span class="muted small" id="pick-count"></span>
          </div>
          <div class="pick-list" id="pick-list"></div>
          <details style="margin-top:14px">
            <summary><strong>Pre-print labels for new bins</strong></summary>
            <p class="muted small" style="margin-top:8px">This creates fresh, unused codes. Stick the labels on bins, then scan each one to set it up.</p>
            <div class="row">
              <label class="field">How many
                <input type="number" id="new-count" min="1" max="1000" value="10" inputmode="numeric" style="width:110px">
              </label>
              <button class="btn" type="button" id="new-codes">Add new codes</button>
            </div>
            <div id="new-list" style="margin-top:8px"></div>
          </details>
        </section>

        <section class="card stack" id="options">
          <h2>2. Label style</h2>
          <div class="seg" role="group" aria-label="Code type">
            <button type="button" data-type="qr">QR code</button>
            <button type="button" data-type="barcode">Barcode</button>
          </div>
          <label class="field">Label paper
            <select id="layout">
              ${[SHEETS, ROLLS].map((g) => html`
                <optgroup label="${g}">
                  ${LAYOUTS.filter((l) => l.group === g).map((l) => html`<option value="${l.id}">${l.name}</option>`)}
                </optgroup>`)}
            </select>
          </label>
          <div id="custom-fields"></div>
          <label class="check"><input type="checkbox" id="show-name"> Show bin name</label>
          <label class="check"><input type="checkbox" id="show-location"> Show location</label>
          <div class="row">
            <label class="field">Copies of each
              <input type="number" id="copies" min="1" max="50" inputmode="numeric" style="width:110px">
            </label>
            <label class="field" id="skip-field">Skip used labels on first sheet
              <input type="number" id="skip" min="0" inputmode="numeric" style="width:110px">
            </label>
          </div>
          <label class="check" id="rotate-field"><input type="checkbox" id="rotate"> Rotate 90° (if labels come out sideways)</label>
          <details id="offset-field">
            <summary>Labels printing off-center?</summary>
            <p class="muted small" style="margin-top:8px">Move everything by a small amount, in inches. Positive values move right and down.</p>
            <div class="row">
              <label class="field">Right <input type="number" id="offset-x" step="0.01" min="-1" max="1" style="width:100px"></label>
              <label class="field">Down <input type="number" id="offset-y" step="0.01" min="-1" max="1" style="width:100px"></label>
            </div>
          </details>
        </section>

        <section class="card">
          <button class="btn primary" type="button" id="print" style="width:100%;min-height:48px">Print</button>
          <p class="muted small" style="margin:10px 0 0">
            In the print dialog, set <strong>Margins: None</strong>, <strong>Scale: 100%</strong> (or Actual size), and turn off <strong>Headers and footers</strong>.
            For a label printer, also choose the matching paper or label size.
          </p>
        </section>
      </div>

      <div>
        <p class="muted small preview-note no-print" id="summary"></p>
        <div class="preview-scroll" id="preview-scroll"><div class="preview-inner" id="preview"></div></div>
      </div>
    </div>`;

  const pageStyle = document.getElementById('page-style');
  const previewEl = $('#preview', view);
  const scrollEl = $('#preview-scroll', view);
  let allBins = [];
  const selected = new Set();
  let newCodes = [];
  let entries = [];
  let layout = resolveLayout(opts);

  // ---- controls <-> options ----
  function writeControls() {
    $$('[data-type]', view).forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.type === opts.type)));
    $('#layout', view).value = layout.id;
    $('#show-name', view).checked = opts.showName;
    $('#show-location', view).checked = opts.showLocation;
    $('#copies', view).value = opts.copies;
    $('#skip', view).value = opts.skip;
    $('#rotate', view).checked = opts.rotate;
    $('#offset-x', view).value = opts.offsetX;
    $('#offset-y', view).value = opts.offsetY;
    renderCustomFields();
  }

  function renderCustomFields() {
    const base = LAYOUTS.find((l) => l.id === opts.layout);
    const box = $('#custom-fields', view);
    const numField = (label, key, value, step = '0.01') => html`
      <label class="field">${label}<input type="number" data-custom="${key}" value="${value}" step="${step}" min="0" style="width:120px"></label>`;
    if (base?.id === 'custom-roll') {
      box.innerHTML = html`<div class="row">
        ${numField('Width (in)', 'w', opts.customRoll.w)}
        ${numField('Height (in)', 'h', opts.customRoll.h)}
      </div>`;
    } else if (base?.id === 'custom-sheet') {
      const c = opts.customSheet;
      box.innerHTML = html`
        <div class="row">
          <label class="field">Paper
            <select data-custom="paper" style="width:120px">
              <option value="letter" ${c.paper !== 'a4' ? 'selected' : ''}>Letter</option>
              <option value="a4" ${c.paper === 'a4' ? 'selected' : ''}>A4</option>
            </select>
          </label>
          ${numField('Labels across', 'cols', c.cols, '1')}
          ${numField('Labels down', 'rows', c.rows, '1')}
          ${numField('Label width (in)', 'w', c.w)}
          ${numField('Label height (in)', 'h', c.h)}
          ${numField('Left margin (in)', 'left', c.left)}
          ${numField('Top margin (in)', 'top', c.top)}
          ${numField('Across pitch (in)', 'pitchX', c.pitchX)}
          ${numField('Down pitch (in)', 'pitchY', c.pitchY)}
        </div>
        <p class="muted small" style="margin-top:6px">Pitch is the distance from the left (or top) edge of one label to the edge of the next. Check the label package for these numbers.</p>`;
    } else {
      box.innerHTML = '';
    }
  }

  function readControls() {
    opts.layout = $('#layout', view).value;
    opts.showName = $('#show-name', view).checked;
    opts.showLocation = $('#show-location', view).checked;
    opts.copies = int($('#copies', view).value, 1, 50, 1);
    opts.skip = int($('#skip', view).value, 0, 999, 0);
    opts.rotate = $('#rotate', view).checked;
    opts.offsetX = num($('#offset-x', view).value, -1, 1, 0);
    opts.offsetY = num($('#offset-y', view).value, -1, 1, 0);
    $$('[data-custom]', view).forEach((input) => {
      const target = opts.layout === 'custom-roll' ? opts.customRoll : opts.customSheet;
      target[input.dataset.custom] = input.value;
    });
    saveOptions(opts);
  }

  // ---- bin picker ----
  function renderPickList() {
    const q = $('#pick-q', view).value.trim().toLowerCase();
    const shown = allBins.filter((b) => !q || `${b.code} ${b.name} ${b.location}`.toLowerCase().includes(q));
    $('#pick-list', view).innerHTML = shown.length ? html`${shown.map((b) => html`
      <label>
        <input type="checkbox" value="${b.code}" ${selected.has(b.code) ? 'checked' : ''}>
        <span class="pick-code">${b.code}</span>
        <span class="pick-name">${b.name || 'Unnamed bin'}${b.location ? ` · ${b.location}` : ''}</span>
      </label>`)}` : html`<p class="muted" style="padding:12px;margin:0">${allBins.length ? 'No bins match.' : 'No bins yet. Use "Pre-print labels" below.'}</p>`;
    return shown;
  }

  function renderNewCodes() {
    $('#new-list', view).innerHTML = newCodes.length ? html`
      <p class="small" style="margin-bottom:6px">${plural(newCodes.length, 'new code')}: <span class="mono">${newCodes[0]}</span>${newCodes.length > 1 ? html` to <span class="mono">${newCodes[newCodes.length - 1]}</span>` : ''}</p>
      <button class="btn small" type="button" id="clear-new">Don't print these</button>` : '';
  }

  // ---- preview ----
  function buildEntries() {
    const chosen = allBins.filter((b) => selected.has(b.code)).map((b) => ({ code: b.code, name: b.name, location: b.location }));
    const fresh = newCodes.map((code) => ({ code, name: '', location: '' }));
    const list = [];
    for (const e of chosen.concat(fresh)) {
      for (let i = 0; i < opts.copies; i++) list.push(e);
    }
    return list;
  }

  function renderPreview() {
    layout = resolveLayout(opts);
    entries = buildEntries();
    const isSheet = layout.kind === 'sheet';
    $('#skip-field', view).hidden = !isSheet;
    $('#offset-field', view).hidden = !isSheet;
    $('#rotate-field', view).hidden = isSheet;
    $('#pick-count', view).textContent = selected.size ? `${selected.size} selected` : '';

    if (!entries.length) {
      pageStyle.textContent = '';
      previewEl.innerHTML = html`<div class="empty" style="margin:0">Choose bins on the left, or add new codes, to preview labels here.</div>`;
      previewEl.style.zoom = 1;
      $('#summary', view).textContent = '';
      return;
    }

    let pages = [];
    let pageW;
    let pageH;
    if (isSheet) {
      pageW = layout.pageW;
      pageH = layout.pageH;
      const perPage = layout.cols * layout.rows;
      const skip = Math.min(opts.skip, perPage - 1);
      const pageCount = Math.ceil((skip + entries.length) / perPage);
      for (let p = 0; p < pageCount; p++) {
        const cells = [];
        for (let slot = 0; slot < perPage; slot++) {
          const idx = p * perPage + slot - skip;
          if (idx < 0 || idx >= entries.length) continue;
          const left = layout.left + (slot % layout.cols) * layout.pitchX + opts.offsetX;
          const top = layout.top + Math.floor(slot / layout.cols) * layout.pitchY + opts.offsetY;
          cells.push(labelHTML(entries[idx], layout, opts, `left:${r4(left)}in;top:${r4(top)}in;width:${r4(layout.w)}in;height:${r4(layout.h)}in;`));
        }
        pages.push(html`<div class="sheet" style="width:${r4(pageW)}in;height:${r4(pageH)}in">${cells}</div>`);
      }
      $('#summary', view).textContent = `${plural(entries.length, 'label')} on ${plural(pageCount, 'sheet')}`;
    } else {
      pageW = opts.rotate ? layout.h : layout.w;
      pageH = opts.rotate ? layout.w : layout.h;
      const rotate = opts.rotate ? `transform-origin:0 0;transform:translateX(${r4(layout.h)}in) rotate(90deg);` : '';
      pages = entries.map((e) => html`
        <div class="roll-page" style="width:${r4(pageW)}in;height:${r4(pageH)}in">
          ${labelHTML(e, layout, opts, `left:0;top:0;width:${r4(layout.w)}in;height:${r4(layout.h)}in;${rotate}`)}
        </div>`);
      $('#summary', view).textContent = plural(entries.length, 'label');
    }

    pageStyle.textContent = `@page { size: ${r4(pageW)}in ${r4(pageH)}in; margin: 0; }`;
    previewEl.innerHTML = html`${pages}`;
    previewEl.dataset.pageWidth = pageW;
    fitPreview();
  }

  function fitPreview() {
    const pageW = parseFloat(previewEl.dataset.pageWidth);
    if (!pageW) return;
    const available = scrollEl.clientWidth - 34;
    previewEl.style.zoom = clamp(available / (pageW * 96), 0.2, 2).toFixed(3);
  }

  const refresh = debounce(() => { readControls(); renderPreview(); }, 120);

  // ---- events ----
  $$('[data-type]', view).forEach((btn) => btn.addEventListener('click', () => {
    opts.type = btn.dataset.type;
    writeControls();
    readControls();
    renderPreview();
  }));
  $('#layout', view).addEventListener('change', () => {
    opts.layout = $('#layout', view).value;
    renderCustomFields();
    readControls();
    renderPreview();
  });
  $('#options', view).addEventListener('input', (e) => { if (e.target.id !== 'layout') refresh(); });
  $('#options', view).addEventListener('change', (e) => { if (e.target.id !== 'layout') refresh(); });

  $('#pick-q', view).addEventListener('input', renderPickList);
  $('#pick-list', view).addEventListener('change', (e) => {
    if (e.target.type !== 'checkbox') return;
    if (e.target.checked) selected.add(e.target.value);
    else selected.delete(e.target.value);
    renderPreview();
  });
  $('#pick-all', view).addEventListener('click', () => {
    const q = $('#pick-q', view).value.trim().toLowerCase();
    allBins.filter((b) => !q || `${b.code} ${b.name} ${b.location}`.toLowerCase().includes(q)).forEach((b) => selected.add(b.code));
    renderPickList();
    renderPreview();
  });
  $('#pick-none', view).addEventListener('click', () => {
    selected.clear();
    renderPickList();
    renderPreview();
  });

  $('#new-codes', view).addEventListener('click', async (e) => {
    const count = int($('#new-count', view).value, 1, 1000, 10);
    e.target.disabled = true;
    try {
      newCodes = newCodes.concat(await api('/codes/reserve', { method: 'POST', body: { count } }));
      renderNewCodes();
      renderPreview();
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      e.target.disabled = false;
    }
  });
  $('#new-list', view).addEventListener('click', (e) => {
    if (e.target.id !== 'clear-new') return;
    newCodes = [];
    renderNewCodes();
    renderPreview();
  });

  $('#print', view).addEventListener('click', async () => {
    if (!entries.length) {
      toast('Choose at least one bin, or add new codes, first.', 'error');
      return;
    }
    const imgs = $$('img', previewEl);
    await Promise.all(imgs.map((img) => (img.complete ? null : new Promise((done) => { img.onload = done; img.onerror = done; }))));
    window.print();
  });

  window.addEventListener('resize', fitPreview);

  // ---- load ----
  writeControls();
  renderPreview();
  allBins = await api('/bins');
  if (!ctx.isCurrent()) return undefined;
  allBins.forEach((b) => { if (preselected.has(b.code)) selected.add(b.code); });
  renderPickList();
  renderPreview();

  return () => {
    window.removeEventListener('resize', fitPreview);
    pageStyle.textContent = '';
  };
}
