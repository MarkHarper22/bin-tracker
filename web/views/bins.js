import {
  $, $$, api, html, icons, esc, binHref, codeImg, go, toast, debounce, setTitle, field, plural,
  openDialog, confirmDialog, createBinDialog, historyList,
} from '../lib.js';

// ---------- bin list ----------

export async function renderBins(view, ctx) {
  setTitle('Bins');
  const q = ctx.params.get('q') || '';
  const location = ctx.params.get('location') || '';

  view.innerHTML = html`
    <div class="page-head">
      <h1>Bins</h1>
      <div class="actions">
        <a class="btn hide-mobile" href="#/labels">${icons.print} Print labels</a>
        <button class="btn primary" type="button" id="new-bin">${icons.plus} New bin</button>
      </div>
    </div>
    <div class="stats" id="stats"></div>
    <div class="toolbar">
      <input class="grow search-big" type="search" id="q" value="${q}"
        placeholder="Search codes, names, locations, or items…" aria-label="Search bins" autocomplete="off">
      <select id="loc" aria-label="Filter by location"><option value="">All locations</option></select>
    </div>
    <div id="results" class="bin-list" aria-live="polite"></div>`;

  const qInput = $('#q', view);
  const locSelect = $('#loc', view);
  $('#new-bin', view).addEventListener('click', () => createBinDialog());

  const [stats, locations] = await Promise.all([api('/stats'), api('/locations')]);
  if (!ctx.isCurrent()) return;

  $('#stats', view).innerHTML = html`
    <div class="stat"><b>${stats.bins.toLocaleString()}</b><span>Bins</span></div>
    <div class="stat"><b>${stats.itemTypes.toLocaleString()}</b><span>Item types</span></div>
    <div class="stat"><b>${stats.totalQty.toLocaleString()}</b><span>Total items</span></div>
    <div class="stat"><b>${stats.locations.toLocaleString()}</b><span>Locations</span></div>`;

  locSelect.innerHTML += html`${locations.map((l) => html`<option value="${l}">${l}</option>`)}`;
  locSelect.value = location;

  let requestId = 0;
  async function load() {
    const id = ++requestId;
    const query = qInput.value.trim();
    const loc = locSelect.value;
    const params = new URLSearchParams();
    if (query) params.set('q', query);
    if (loc) params.set('location', loc);
    history.replaceState(null, '', '#/bins' + (params.toString() ? '?' + params : ''));

    const bins = await api('/bins?' + params);
    if (id !== requestId || !ctx.isCurrent()) return;
    $('#results', view).innerHTML = renderRows(bins, stats.bins === 0, query || loc);
    $('#empty-new', view)?.addEventListener('click', () => createBinDialog());
  }

  qInput.addEventListener('input', debounce(() => load().catch((e) => toast(e.message, 'error')), 200));
  locSelect.addEventListener('change', () => load().catch((e) => toast(e.message, 'error')));
  await load();
}

function renderRows(bins, noBinsAtAll, filtered) {
  if (noBinsAtAll) {
    return html`
      <div class="empty">
        <h2>No bins yet</h2>
        <p>Create your first bin, or scan a pre-printed label to add it.</p>
        <button class="btn primary" type="button" id="empty-new">${icons.plus} New bin</button>
        <a class="btn" href="#/scan">${icons.scan} Scan a label</a>
      </div>`;
  }
  if (!bins.length) {
    return html`<div class="empty"><h2>No matches</h2><p>${filtered ? 'Try a different search or location.' : ''}</p></div>`;
  }
  return html`${bins.map((b) => html`
    <a class="bin-row" href="${binHref(b.code)}">
      <span class="code">${b.code}</span>
      <span class="name">${b.name || html`<span class="muted">Unnamed bin</span>`}</span>
      <span class="meta">${plural(b.itemCount, 'item')} · ${b.totalQty.toLocaleString()} total</span>
      <span class="loc">${icons.pin}${b.location || 'No location'}</span>
      ${b.matchedItems?.length ? html`<span class="matches">${b.matchedItems.map((m) => html`<span class="chip">${m}</span>`)}</span>` : ''}
    </a>`)}`;
}

// ---------- single bin ----------

export async function renderBin(view, code, ctx) {
  setTitle(code);
  let bin;
  try {
    bin = await api('/bins/' + encodeURIComponent(code));
  } catch (err) {
    if (!ctx.isCurrent()) return;
    if (err.status !== 404 && err.status !== 400) throw err;
    view.innerHTML = html`
      <a class="back-link" href="#/bins">${icons.back} All bins</a>
      <div class="empty">
        <h2>No bin with code ${code}</h2>
        <p>It may have been deleted, or its label hasn't been set up yet.</p>
        ${err.status === 404 ? html`<button class="btn primary" type="button" id="create">${icons.plus} Create bin ${code}</button>` : ''}
      </div>`;
    $('#create', view)?.addEventListener('click', () => createBinDialog(code.toUpperCase()));
    return;
  }
  const locations = await api('/locations').catch(() => []);
  if (!ctx.isCurrent()) return;

  view.innerHTML = html`
    <a class="back-link" href="#/bins">${icons.back} All bins</a>
    <div class="page-head">
      <div class="bin-title">
        <span class="code">${bin.code}</span>
        <h1 id="bin-name"></h1>
        <span class="muted" id="bin-summary"></span>
      </div>
      <div class="actions">
        <a class="btn" href="#/labels?bins=${encodeURIComponent(bin.code)}">${icons.print} Print label</a>
        <button class="btn danger" type="button" id="delete">${icons.trash} Delete</button>
      </div>
    </div>

    <div class="two-col">
      <section class="card">
        <div class="card-head"><h2>Contents</h2></div>
        <div id="items"></div>
        <form class="add-item" id="add-item" autocomplete="off">
          <input type="text" name="name" maxlength="200" placeholder="Add an item, e.g. AA batteries" aria-label="Item name" required>
          <input type="number" name="qty" min="1" value="1" aria-label="Quantity" inputmode="numeric" required>
          <button class="btn primary" type="submit">${icons.plus}<span class="hide-mobile">Add</span></button>
        </form>
        <p class="muted small" style="margin:8px 0 0">Adding an item that's already in the bin increases its quantity.</p>
      </section>

      <div>
        <section class="card">
          <div class="card-head">
            <h2>Details</h2>
          </div>
          <form id="details" class="stack" autocomplete="off">
            <label class="field">Name
              <input type="text" name="name" maxlength="200" value="${bin.name}" placeholder="e.g. Holiday lights">
            </label>
            <label class="field">Location
              <input type="text" name="location" maxlength="200" value="${bin.location}" list="bin-locations" placeholder="e.g. Garage shelf 2">
            </label>
            <label class="field">Notes
              <textarea name="notes" maxlength="5000" placeholder="Anything else worth knowing">${bin.notes}</textarea>
            </label>
            <div class="row">
              <button class="btn primary" type="submit" id="save" disabled>Save changes</button>
              <div class="spacer"></div>
              <div class="bin-code-preview">
                <img src="${codeImg('qr', bin.code)}" alt="QR code for ${bin.code}">
              </div>
            </div>
            <datalist id="bin-locations">${locations.map((l) => html`<option value="${l}">`)}</datalist>
          </form>
        </section>

        <section class="card">
          <div class="card-head">
            <h2>History</h2>
            <div class="actions"><a class="btn small ghost" href="#/history?bin=${encodeURIComponent(bin.code)}">See all</a></div>
          </div>
          <div id="history"></div>
        </section>
      </div>
    </div>`;

  const itemsEl = $('#items', view);
  const detailsForm = $('#details', view);
  const addForm = $('#add-item', view);
  const saveBtn = $('#save', view);

  function renderHeader() {
    $('#bin-name', view).innerHTML = bin.name ? esc(bin.name) : '<span class="muted">Unnamed bin</span>';
    $('#bin-summary', view).textContent =
      `${bin.location ? bin.location + ' · ' : ''}${plural(bin.itemCount, 'item type')}, ${bin.totalQty.toLocaleString()} total`;
    setTitle(bin.name ? `${bin.code} ${bin.name}` : bin.code);
  }

  function renderItems() {
    if (!bin.items.length) {
      itemsEl.innerHTML = html`<p class="muted">This bin is empty. Add what's inside below.</p>`;
      return;
    }
    itemsEl.innerHTML = html`
      <table class="items-table"><tbody>
        ${bin.items.map((it) => html`
          <tr data-id="${it.id}">
            <td class="item-name"><button type="button" data-act="rename" title="Rename">${it.name}</button></td>
            <td>
              <div class="qty-ctl">
                <button type="button" data-act="dec" aria-label="One fewer ${it.name}">−</button>
                <input type="number" min="0" value="${it.quantity}" inputmode="numeric" aria-label="Quantity of ${it.name}">
                <button type="button" data-act="inc" aria-label="One more ${it.name}">+</button>
              </div>
            </td>
            <td><button class="btn ghost icon small danger" type="button" data-act="delete" aria-label="Remove ${it.name}">${icons.trash}</button></td>
          </tr>`)}
      </tbody></table>`;
  }

  async function renderHistory() {
    const entries = await api('/history?limit=15&bin=' + encodeURIComponent(bin.code));
    if (ctx.isCurrent()) $('#history', view).innerHTML = historyList(entries, { showCode: false });
  }

  function applyBin(updated, { items = false } = {}) {
    bin = updated;
    renderHeader();
    if (items) renderItems();
    renderHistory().catch(() => {});
  }

  renderHeader();
  renderItems();
  renderHistory().catch(() => {});

  // --- details form ---
  const dirty = () =>
    field(detailsForm, 'name').value.trim() !== bin.name ||
    field(detailsForm, 'location').value.trim() !== bin.location ||
    field(detailsForm, 'notes').value.trim() !== bin.notes;
  detailsForm.addEventListener('input', () => { saveBtn.disabled = !dirty(); });
  detailsForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    saveBtn.disabled = true;
    try {
      const updated = await api('/bins/' + encodeURIComponent(bin.code), {
        method: 'PATCH',
        body: {
          name: field(detailsForm, 'name').value,
          location: field(detailsForm, 'location').value,
          notes: field(detailsForm, 'notes').value,
        },
      });
      applyBin(updated);
      field(detailsForm, 'name').value = bin.name;
      field(detailsForm, 'location').value = bin.location;
      field(detailsForm, 'notes').value = bin.notes;
      toast('Saved');
    } catch (err) {
      toast(err.message, 'error');
      saveBtn.disabled = !dirty();
    }
  });

  // --- add item ---
  addForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    const nameInput = field(addForm, 'name');
    const qtyInput = field(addForm, 'qty');
    try {
      const updated = await api(`/bins/${encodeURIComponent(bin.code)}/items`, {
        method: 'POST',
        body: { name: nameInput.value, quantity: parseInt(qtyInput.value, 10) || 1 },
      });
      applyBin(updated, { items: true });
      nameInput.value = '';
      qtyInput.value = '1';
      nameInput.focus();
    } catch (err) {
      toast(err.message, 'error');
    }
  });

  // --- item rows: quantity, rename, delete ---
  const pendingQty = new Map();
  const sendQty = debounce(async () => {
    const changes = [...pendingQty.entries()];
    pendingQty.clear();
    for (const [id, quantity] of changes) {
      try {
        applyBin(await api('/items/' + id, { method: 'PATCH', body: { quantity } }));
      } catch (err) {
        toast(err.message, 'error');
        applyBin(await api('/bins/' + encodeURIComponent(bin.code)), { items: true });
      }
    }
  }, 600);

  function queueQty(row, value) {
    const qty = Math.max(0, Number.isFinite(value) ? value : 0);
    $('input', row).value = qty;
    pendingQty.set(row.dataset.id, qty);
    sendQty();
  }

  itemsEl.addEventListener('click', async (e) => {
    const btn = e.target.closest('button[data-act]');
    if (!btn) return;
    const row = btn.closest('tr');
    const id = row.dataset.id;
    const item = bin.items.find((it) => String(it.id) === id);
    const input = $('input', row);
    switch (btn.dataset.act) {
      case 'inc': queueQty(row, parseInt(input.value, 10) + 1); break;
      case 'dec': queueQty(row, parseInt(input.value, 10) - 1); break;
      case 'rename': {
        const updated = await openDialog({
          title: 'Rename item',
          confirmText: 'Rename',
          body: html`<label class="field">Name<input type="text" name="name" maxlength="200" value="${item.name}"></label>`,
          onConfirm: (form) => api('/items/' + id, { method: 'PATCH', body: { name: field(form, 'name').value } }),
        });
        if (updated) applyBin(updated, { items: true });
        break;
      }
      case 'delete': {
        if (!(await confirmDialog('Remove item?', `Remove "${item.name}" (${item.quantity}) from ${bin.code}?`, 'Remove', true))) return;
        try {
          applyBin(await api('/items/' + id, { method: 'DELETE' }), { items: true });
        } catch (err) {
          toast(err.message, 'error');
        }
        break;
      }
    }
  });
  itemsEl.addEventListener('change', (e) => {
    if (e.target.matches('.qty-ctl input')) queueQty(e.target.closest('tr'), parseInt(e.target.value, 10));
  });

  // --- delete bin ---
  $('#delete', view).addEventListener('click', async () => {
    const ok = await confirmDialog(
      `Delete ${bin.code}?`,
      `This removes the bin and its ${plural(bin.itemCount, 'item type')} from the inventory. The history log keeps a record. You can reuse the label to create the bin again later.`,
      'Delete bin', true);
    if (!ok) return;
    try {
      await api('/bins/' + encodeURIComponent(bin.code), { method: 'DELETE' });
      toast(`Deleted ${bin.code}`);
      go('#/bins');
    } catch (err) {
      toast(err.message, 'error');
    }
  });

  // Flush a pending quantity change if the user leaves quickly.
  return () => { if (pendingQty.size) sendQty(); };
}
