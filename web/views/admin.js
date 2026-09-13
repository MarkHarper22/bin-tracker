import {
  $, api, html, toast, setTitle, field, debounce, when, codeImg, historyList,
  confirmDialog, deviceName, setDeviceName, go, phoneLink,
} from '../lib.js';

// ---------- history ----------

export async function renderHistory(view, ctx) {
  setTitle('History');
  const bin = ctx.params.get('bin') || '';

  view.innerHTML = html`
    <div class="page-head">
      <h1>History</h1>
    </div>
    ${bin ? html`<p>Showing changes to <a class="mono" href="#/bin/${encodeURIComponent(bin)}">${bin}</a> · <a href="#/history">Show all bins</a></p>` : ''}
    <div class="toolbar">
      <input class="grow search-big" type="search" id="q" placeholder="Search history (code, item, location, person…)" aria-label="Search history" autocomplete="off">
    </div>
    <section class="card">
      <div id="list"></div>
      <button class="btn" type="button" id="more" hidden>Load older entries</button>
    </section>`;

  const listEl = $('#list', view);
  const moreBtn = $('#more', view);
  const qInput = $('#q', view);
  const PAGE = 100;
  let entries = [];
  let requestId = 0;

  async function load(append = false) {
    const id = ++requestId;
    const params = new URLSearchParams({ limit: PAGE });
    if (bin) params.set('bin', bin);
    if (qInput.value.trim()) params.set('q', qInput.value.trim());
    if (append && entries.length) params.set('before', entries[entries.length - 1].id);
    const page = await api('/history?' + params);
    if (id !== requestId || !ctx.isCurrent()) return;
    entries = append ? entries.concat(page) : page;
    listEl.innerHTML = historyList(entries, { showCode: !bin });
    moreBtn.hidden = page.length < PAGE;
  }

  qInput.addEventListener('input', debounce(() => load().catch((e) => toast(e.message, 'error')), 250));
  moreBtn.addEventListener('click', () => load(true).catch((e) => toast(e.message, 'error')));
  await load();
}

// ---------- settings ----------

function formatBytes(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

const KIND_LABELS = { auto: 'Automatic', manual: 'Manual', 'before-restore': 'Before a restore' };

export async function renderSettings(view, ctx) {
  setTitle('Settings');
  const [info, settings, backups] = await Promise.all([api('/info'), api('/settings'), api('/backups')]);
  if (!ctx.isCurrent()) return;
  const phoneUrl = phoneLink(info);

  view.innerHTML = html`
    <div class="page-head"><h1>Settings</h1></div>

    <div class="two-col">
      <section class="card">
        <h2>This device</h2>
        <form id="device" class="row" autocomplete="off">
          <label class="field grow">Your name or device name (shown in history)
            <input type="text" name="device" maxlength="40" value="${deviceName()}">
          </label>
          <button class="btn primary" type="submit">Save</button>
        </form>
      </section>

      <section class="card">
        <h2>Bin codes</h2>
        <form id="codes" class="row" autocomplete="off">
          <label class="field grow">Code prefix for new bins
            <input type="text" name="prefix" maxlength="12" value="${settings.codePrefix}" autocapitalize="characters">
          </label>
          <button class="btn primary" type="submit">Save</button>
        </form>
        <p class="muted small" style="margin-top:8px" id="next-code">Next new bin: <span class="mono">${settings.codePrefix}${String(settings.nextNumber).padStart(5, '0')}</span></p>
      </section>
    </div>

    <section class="card">
      <h2>Connect a phone</h2>
      ${phoneUrl ? html`
        <div class="connect">
          <img src="${codeImg('qr', phoneUrl)}" alt="QR code linking to ${phoneUrl}">
          <ol>
            <li>Connect the phone to the <strong>same network</strong> as ${info.serverMode ? 'the server' : 'this computer'}.</li>
            <li>Point the phone's camera at this QR code and tap the link, or type <strong class="mono">${phoneUrl}</strong> into its browser.</li>
            <li>If the phone warns that the connection isn't private, that's expected: Bin Tracker creates its own security certificate for your network.
              On iPhone tap <em>Show Details → visit this website</em>. On Android tap <em>Advanced → Proceed</em>.</li>
            <li>Allow camera access when the Scan screen asks.</li>
            <li>Optional: use <em>Share → Add to Home Screen</em> so it opens like an app.</li>
          </ol>
        </div>
        ${info.phoneUrls.length > 1 ? html`<p class="muted small" style="margin-top:10px">Other addresses: ${info.phoneUrls.slice(1).join(', ')}</p>` : ''}
        <p class="muted small" style="margin-top:10px">${info.serverMode
          ? `If the phone can't connect, check that port ${info.httpsPort} is published by Docker and allowed through the server's firewall.`
          : "If the phone can't connect, check that the computer's firewall allows Bin Tracker on private networks. Windows asks about this the first time the app runs."}</p>
      ` : info.serverMode
        ? html`<p class="muted">To show a phone link here, open Bin Tracker by the server's network address (for example http://192.168.1.50:8420) instead of localhost, or set <span class="mono">BINTRACKER_PHONE_URL</span> on the container.</p>`
        : html`<p class="muted">This computer doesn't appear to be connected to a network, so phones can't reach it right now.</p>`}
    </section>

    <section class="card">
      <div class="card-head">
        <h2>Backups</h2>
        <div class="actions"><button class="btn" type="button" id="backup-now">Back up now</button></div>
      </div>
      <form id="backup-settings" class="row" autocomplete="off">
        <label class="field">Automatic backup every (hours, 0 = off)
          <input type="number" name="hours" min="0" max="720" value="${settings.backupHours}" inputmode="numeric">
        </label>
        <label class="field">Automatic backups to keep
          <input type="number" name="keep" min="1" max="365" value="${settings.backupKeep}" inputmode="numeric">
        </label>
        <button class="btn primary" type="submit">Save</button>
      </form>
      <p class="muted small" style="margin:10px 0">
        Last backup: ${settings.lastBackup ? when(settings.lastBackup) : 'none yet'}.
        A backup is only made when something has changed.
        Backups are stored in <span class="mono">${info.dataDir}</span>. ${info.serverMode
          ? 'For extra safety, include that volume in your server backups.'
          : 'For extra safety, copy that folder to a USB drive or cloud folder now and then.'}
      </p>
      <div class="table-wrap" id="backup-list"></div>
    </section>

    <section class="card">
      <h2>About</h2>
      <dl class="kv">
        <dt>Version</dt><dd>${info.version}</dd>
        ${info.serverMode
          ? html`<dt>Mode</dt><dd>Server (Docker)</dd>`
          : html`<dt>On this computer</dt><dd class="mono">${info.localUrl}</dd>`}
        <dt>Data folder</dt><dd class="mono">${info.dataDir}</dd>
      </dl>
      ${info.isLocal ? html`
        <p class="muted small" style="margin:12px 0 8px">Stopping Bin Tracker disconnects every phone and computer using it until it's started again.</p>
        <button class="btn danger" type="button" id="shutdown">Stop Bin Tracker</button>` : ''}
    </section>`;

  // --- device name ---
  $('#device', view).addEventListener('submit', (e) => {
    e.preventDefault();
    setDeviceName(field(e.target, 'device').value);
    field(e.target, 'device').value = deviceName();
    toast('Device name saved');
  });

  // --- code prefix ---
  $('#codes', view).addEventListener('submit', async (e) => {
    e.preventDefault();
    try {
      const s = await api('/settings', { method: 'PATCH', body: { codePrefix: field(e.target, 'prefix').value } });
      field(e.target, 'prefix').value = s.codePrefix;
      $('#next-code', view).innerHTML = html`Next new bin: <span class="mono">${s.codePrefix}${String(s.nextNumber).padStart(5, '0')}</span>`;
      toast('Code prefix saved');
    } catch (err) {
      toast(err.message, 'error');
    }
  });

  // --- backups ---
  function renderBackups(list) {
    $('#backup-list', view).innerHTML = list.length ? html`
      <table class="table">
        <thead><tr><th>Date</th><th>Type</th><th>Size</th><th></th></tr></thead>
        <tbody>${list.map((b) => html`
          <tr>
            <td>${when(b.created)}</td>
            <td>${KIND_LABELS[b.kind] || b.kind}</td>
            <td>${formatBytes(b.size)}</td>
            <td style="text-align:right"><button class="btn small" type="button" data-restore="${b.name}">Restore</button></td>
          </tr>`)}
        </tbody>
      </table>` : html`<p class="muted">No backups yet.</p>`;
  }
  renderBackups(backups);

  $('#backup-settings', view).addEventListener('submit', async (e) => {
    e.preventDefault();
    try {
      await api('/settings', {
        method: 'PATCH',
        body: {
          backupHours: parseInt(field(e.target, 'hours').value, 10),
          backupKeep: parseInt(field(e.target, 'keep').value, 10),
        },
      });
      toast('Backup settings saved');
    } catch (err) {
      toast(err.message, 'error');
    }
  });

  $('#backup-now', view).addEventListener('click', async (e) => {
    e.target.disabled = true;
    try {
      await api('/backups', { method: 'POST' });
      renderBackups(await api('/backups'));
      toast('Backup saved');
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      e.target.disabled = false;
    }
  });

  $('#backup-list', view).addEventListener('click', async (e) => {
    const name = e.target.closest('[data-restore]')?.dataset.restore;
    if (!name) return;
    const b = backups.find((x) => x.name === name);
    const ok = await confirmDialog(
      'Restore this backup?',
      `All bins and history will go back to how they were on ${b ? when(b.created) : name}. Changes made since then are replaced. A backup of the current data is saved first, so this can be undone.`,
      'Restore', true);
    if (!ok) return;
    try {
      await api(`/backups/${encodeURIComponent(name)}/restore`, { method: 'POST' });
      toast('Backup restored');
      go('#/bins');
    } catch (err) {
      toast(err.message, 'error');
    }
  });

  // --- shutdown ---
  $('#shutdown', view)?.addEventListener('click', async () => {
    if (!(await confirmDialog('Stop Bin Tracker?', 'Phones and other computers will lose access until it is started again.', 'Stop', true))) return;
    try {
      await api('/shutdown', { method: 'POST' });
    } catch { /* the server may close before replying */ }
    view.innerHTML = html`<div class="empty"><h2>Bin Tracker has stopped</h2><p>Open the app again to keep working. You can close this tab.</p></div>`;
  });
}
