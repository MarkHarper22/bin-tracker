import { $, $$, api, html, icons, setTitle, field, openCode, setScanHandler, extractCode, phoneLink } from '../lib.js';

const DUPLICATE_WINDOW_MS = 2500;

export async function renderScan(view, ctx) {
  setTitle('Scan');
  const liveSupported = window.isSecureContext && !!navigator.mediaDevices?.getUserMedia;

  view.innerHTML = html`
    <div class="page-head"><h1>Scan</h1></div>
    <div class="two-col">
      <section class="card">
        <div class="scanner" id="scanner">
          <video id="video" playsinline muted hidden></video>
          <div class="scan-frame" hidden></div>
          <div class="scan-idle" id="idle">
            ${icons.camera}
            ${liveSupported ? html`<button class="btn primary" type="button" id="start">Start camera</button>` : ''}
            <label class="btn">
              Take a photo of a label
              <input type="file" accept="image/*" capture="environment" id="photo" hidden>
            </label>
          </div>
        </div>
        <div class="row" style="margin-top:10px">
          <button class="btn" type="button" id="stop" hidden>Stop camera</button>
          <select id="camera-select" hidden aria-label="Choose camera" style="width:auto"></select>
        </div>
        <p class="scan-status" id="status" role="status"></p>
        <div id="secure-note"></div>
      </section>

      <section class="card stack">
        <h2>What should a scan do?</h2>
        <div class="seg" role="group" aria-label="Scan mode">
          <button type="button" data-mode="open" aria-pressed="true">Open bin</button>
          <button type="button" data-mode="move" aria-pressed="false">Move bins</button>
        </div>
        <div id="mode-open">
          <p class="muted">Scan a label to open that bin. If the code is new, you can create the bin on the spot.</p>
        </div>
        <div id="mode-move" hidden>
          <p class="muted">Enter a location, then scan bins one after another to move them all there.</p>
          <label class="field">Move scanned bins to
            <input type="text" id="move-to" maxlength="200" list="scan-locations" placeholder="e.g. Basement rack 3" autocomplete="off">
          </label>
          <datalist id="scan-locations"></datalist>
          <ul class="moved-list" id="moved"></ul>
        </div>
        <form id="manual" class="row" autocomplete="off">
          <label class="field grow">Type or scan a code
            <input type="text" name="code" maxlength="40" autocapitalize="characters" placeholder="BIN-00001">
          </label>
          <button class="btn primary" type="submit">Go</button>
        </form>
        <p class="muted small">Handheld scanners work on every screen. Scan while no text box is selected, or scan into the box above.</p>
      </section>
    </div>`;

  const video = $('#video', view);
  const scanner = $('#scanner', view);
  const statusEl = $('#status', view);
  const idle = $('#idle', view);
  const stopBtn = $('#stop', view);
  const cameraSelect = $('#camera-select', view);
  const moveTo = $('#move-to', view);
  let mode = 'open';

  function setStatus(message, type = '') {
    statusEl.textContent = message;
    statusEl.className = 'scan-status' + (type ? ' ' + type : '');
  }

  // ---- modes ----
  $$('[data-mode]', view).forEach((btn) => btn.addEventListener('click', () => {
    mode = btn.dataset.mode;
    $$('[data-mode]', view).forEach((b) => b.setAttribute('aria-pressed', String(b === btn)));
    $('#mode-open', view).hidden = mode !== 'open';
    $('#mode-move', view).hidden = mode !== 'move';
    if (mode === 'move') moveTo.focus();
    setStatus('');
  }));

  api('/locations').then((locs) => {
    $('#scan-locations', view).innerHTML = html`${locs.map((l) => html`<option value="${l}">`)}`;
  }).catch(() => {});

  async function moveBin(code) {
    const dest = moveTo.value.trim();
    if (!dest) {
      setStatus('Enter where the bins are going first.', 'error');
      moveTo.focus();
      return;
    }
    try {
      const bin = await api('/bins/' + encodeURIComponent(code), { method: 'PATCH', body: { location: dest } });
      const li = document.createElement('li');
      li.innerHTML = html`<strong class="mono">${bin.code}</strong><span>${bin.name || 'Unnamed bin'}</span><span class="spacer"></span><span class="muted small">→ ${dest}</span>`;
      $('#moved', view).prepend(li);
      setStatus(`Moved ${bin.code} to ${dest}`, 'ok');
    } catch (err) {
      setStatus(err.status === 404 ? `No bin with code ${code}. Create it first in Open bin mode.` : err.message, 'error');
    }
  }

  function handle(code) {
    code = extractCode(code);
    if (!code) return;
    if (mode === 'move') moveBin(code);
    else openCode(code);
  }
  setScanHandler(handle);

  $('#manual', view).addEventListener('submit', (e) => {
    e.preventDefault();
    const input = field(e.target, 'code');
    handle(input.value);
    input.value = '';
  });

  // ---- decoding ----
  let detector = null;
  if ('BarcodeDetector' in window) {
    try {
      const supported = await window.BarcodeDetector.getSupportedFormats();
      const wanted = ['qr_code', 'code_128', 'code_39', 'ean_13', 'upc_a'].filter((f) => supported.includes(f));
      if (wanted.includes('qr_code') && wanted.includes('code_128')) detector = new window.BarcodeDetector({ formats: wanted });
    } catch { /* fall back to the server decoder */ }
  }

  const canvas = document.createElement('canvas');
  async function frameToJpeg(source, width, height, maxSide) {
    const scale = Math.min(1, maxSide / Math.max(width, height));
    canvas.width = Math.round(width * scale);
    canvas.height = Math.round(height * scale);
    canvas.getContext('2d').drawImage(source, 0, 0, canvas.width, canvas.height);
    return new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.85));
  }

  async function decodeOnServer(blob) {
    const res = await api('/decode', { method: 'POST', blob });
    return res.found ? res.text : null;
  }

  let lastCode = '';
  let lastAt = 0;
  function detected(text) {
    const now = Date.now();
    if (text === lastCode && now - lastAt < DUPLICATE_WINDOW_MS) return;
    lastCode = text;
    lastAt = now;
    scanner.classList.add('hit');
    setTimeout(() => scanner.classList.remove('hit'), 400);
    navigator.vibrate?.(60);
    handle(text);
  }

  // ---- live camera ----
  let stream = null;
  let running = false;
  let timer = null;

  async function start(deviceId) {
    stopCamera();
    setStatus('Starting camera…');
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: false,
        video: deviceId
          ? { deviceId: { exact: deviceId }, width: { ideal: 1280 }, height: { ideal: 720 } }
          : { facingMode: { ideal: 'environment' }, width: { ideal: 1280 }, height: { ideal: 720 } },
      });
    } catch (err) {
      setStatus(err.name === 'NotAllowedError'
        ? 'Camera access was blocked. Allow camera access for this site in your browser settings, then try again.'
        : `Couldn't start the camera (${err.message}).`, 'error');
      return;
    }
    if (!ctx.isCurrent()) { stopCamera(); return; }
    video.srcObject = stream;
    await video.play().catch(() => {});
    video.hidden = false;
    $('.scan-frame', view).hidden = false;
    idle.hidden = true;
    stopBtn.hidden = false;
    setStatus('Point the camera at a label.');
    running = true;
    tick();

    const cams = (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'videoinput');
    if (cams.length > 1) {
      const current = stream.getVideoTracks()[0]?.getSettings().deviceId;
      cameraSelect.innerHTML = html`${cams.map((c, i) => html`<option value="${c.deviceId}">${c.label || `Camera ${i + 1}`}</option>`)}`;
      if (current) cameraSelect.value = current;
      cameraSelect.hidden = false;
    }
  }

  function stopCamera() {
    running = false;
    clearTimeout(timer);
    stream?.getTracks().forEach((t) => t.stop());
    stream = null;
    video.srcObject = null;
    video.hidden = true;
    $('.scan-frame', view).hidden = true;
    idle.hidden = false;
    stopBtn.hidden = true;
  }

  async function tick() {
    if (!running) return;
    // Pause while a dialog (like "create bin") is open.
    if (video.readyState >= 2 && !document.querySelector('dialog[open]')) {
      try {
        let text = null;
        if (detector) {
          const found = await detector.detect(video);
          text = found[0]?.rawValue || null;
        } else {
          const blob = await frameToJpeg(video, video.videoWidth, video.videoHeight, 1024);
          if (blob && running) text = await decodeOnServer(blob);
        }
        if (text && running) detected(text);
      } catch (err) {
        if (err.status === undefined && /reach/.test(err.message)) setStatus(err.message, 'error');
      }
    }
    if (running) timer = setTimeout(tick, detector ? 150 : 250);
  }

  $('#start', view)?.addEventListener('click', () => start());
  stopBtn.addEventListener('click', () => { stopCamera(); setStatus(''); });
  cameraSelect.addEventListener('change', () => start(cameraSelect.value));

  // ---- photo fallback (works without HTTPS) ----
  $('#photo', view).addEventListener('change', async (e) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    setStatus('Reading photo…');
    const url = URL.createObjectURL(file);
    try {
      const img = new Image();
      img.src = url;
      await img.decode();
      const blob = await frameToJpeg(img, img.naturalWidth, img.naturalHeight, 1600);
      let text = null;
      if (detector) {
        const found = await detector.detect(img).catch(() => []);
        text = found[0]?.rawValue || null;
      }
      if (!text) text = await decodeOnServer(blob);
      if (text) {
        setStatus(`Found ${extractCode(text)}`, 'ok');
        handle(text);
      } else {
        setStatus('No code found in that photo. Try again closer to the label, in good light.', 'error');
      }
    } catch (err) {
      setStatus(err.message || "Couldn't read that photo.", 'error');
    } finally {
      URL.revokeObjectURL(url);
    }
  });

  // ---- explain the HTTPS requirement on phones ----
  if (!liveSupported) {
    const info = await api('/info').catch(() => null);
    const secureUrl = phoneLink(info);
    if (ctx.isCurrent()) {
      $('#secure-note', view).innerHTML = html`
        <p class="muted small">
          Live camera scanning needs the secure address.
          ${secureUrl ? html`<a href="${secureUrl}/#/scan">Open ${secureUrl.replace('https://', '')}</a>.` : ''}
          You can still scan by taking a photo of a label.
        </p>`;
    }
  }

  return stopCamera;
}
