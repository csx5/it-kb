/**
 * Verwaltungs-UI (Synonyme, Kategorien, Wartung/Einstellungen)
 * sowie Versionshistorie-Modal mit zeilenweisem Diff & 1-Klick-Wiederherstellung.
 */

function escapeHTML(s) {
  return String(s ?? "").replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

/**
 * Berechnet einen kompakten zeilenweisen LCS-Diff zwischen zwei Texten.
 */
export function computeLineDiff(oldText, newText) {
  const a = String(oldText || "").split("\n");
  const b = String(newText || "").split("\n");
  const n = a.length;
  const m = b.length;

  // Bei sehr großen Dokumenten einfacher Zeilenvergleich zur Laufzeitbegrenzung
  if (n * m > 200000) {
    const out = [];
    const maxL = Math.max(n, m);
    for (let i = 0; i < maxL; i++) {
      if (a[i] === b[i]) {
        out.push({ type: "same", text: a[i] ?? "" });
      } else {
        if (i < n) out.push({ type: "del", text: a[i] });
        if (i < m) out.push({ type: "add", text: b[i] });
      }
    }
    return out;
  }

  const dp = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
    }
  }

  const ops = [];
  let i = 0, j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      ops.push({ type: "same", text: a[i] });
      i++;
      j++;
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      ops.push({ type: "del", text: a[i] });
      i++;
    } else {
      ops.push({ type: "add", text: b[j] });
      j++;
    }
  }
  while (i < n) ops.push({ type: "del", text: a[i++] });
  while (j < m) ops.push({ type: "add", text: b[j++] });
  return ops;
}

function renderDiffBlock(label, oldVal, newVal) {
  if ((oldVal || "") === (newVal || "")) return "";
  const lines = computeLineDiff(oldVal, newVal)
    .map((op) => {
      const cls = op.type === "add" ? "diff-line-add" : op.type === "del" ? "diff-line-del" : "diff-line-same";
      const prefix = op.type === "add" ? "+ " : op.type === "del" ? "- " : "  ";
      return `<div class="${cls}">${escapeHTML(prefix + op.text)}</div>`;
    })
    .join("");
  return `
    <div class="diff-field">
      <div class="diff-field-title">${escapeHTML(label)}</div>
      <div class="line-diff-body">${lines}</div>
    </div>
  `;
}

export function createAdminAndHistoryController({ apiRequest, getAuthor, getMeta, reloadMeta, onRestoredArticle, showToast }) {
  // --- Versionshistorie ---
  const revDialog = document.getElementById("revisions-dialog");
  const revList = document.getElementById("rev-list");
  const revDiff = document.getElementById("rev-diff");
  const revSelLeft = document.getElementById("rev-sel-left");
  const revSelRight = document.getElementById("rev-sel-right");
  const revRestoreBtn = document.getElementById("rev-restore-btn");
  const revCloseBtn = document.getElementById("rev-close-btn");

  let activeArticleId = null;
  let activeRevisions = [];

  async function openHistory(articleId) {
    activeArticleId = articleId;
    const { ok, body } = await apiRequest(`/api/articles/${articleId}/revisions`);
    if (!ok || !body?.revisions) {
      showToast("Versionshistorie konnte nicht geladen werden");
      return;
    }
    activeRevisions = body.revisions;
    renderRevisionSelectors();
    renderRevisionList();
    renderRevisionDiff();
    revDialog.showModal();
  }

  function renderRevisionSelectors() {
    revSelLeft.innerHTML = "";
    revSelRight.innerHTML = "";
    for (const r of activeRevisions) {
      const dateStr = new Date(r.created_at).toLocaleString("de-DE");
      const label = `v${r.version} (${dateStr}, ${r.editor_name || "Unbekannt"})`;
      const o1 = new Option(label, String(r.version));
      const o2 = new Option(label, String(r.version));
      revSelLeft.add(o1);
      revSelRight.add(o2);
    }
    if (activeRevisions.length >= 2) {
      revSelLeft.value = String(activeRevisions[1].version);
      revSelRight.value = String(activeRevisions[0].version);
    } else if (activeRevisions.length === 1) {
      revSelLeft.value = String(activeRevisions[0].version);
      revSelRight.value = String(activeRevisions[0].version);
    }
  }

  function renderRevisionList() {
    revList.innerHTML = "";
    for (const r of activeRevisions) {
      const row = document.createElement("div");
      row.className = "rev-item";
      const dateStr = new Date(r.created_at).toLocaleString("de-DE");
      row.innerHTML = `
        <div>
          <strong>Version ${r.version}</strong>
          <span class="badge">${escapeHTML(r.editor_name || "Unbekannt")}</span>
          <span class="rev-date">${escapeHTML(dateStr)}</span>
          ${r.change_summary ? `<div class="rev-summary">${escapeHTML(r.change_summary)}</div>` : ""}
        </div>
        <button type="button" class="btn btn-sm" data-pick-rev="${r.version}">Vergleichen</button>
      `;
      row.querySelector("[data-pick-rev]").addEventListener("click", () => {
        revSelLeft.value = String(r.version);
        revSelRight.value = String(activeRevisions[0].version);
        renderRevisionDiff();
      });
      revList.appendChild(row);
    }
  }

  function renderRevisionDiff() {
    const leftV = Number(revSelLeft.value);
    const rightV = Number(revSelRight.value);
    const left = activeRevisions.find((r) => r.version === leftV);
    const right = activeRevisions.find((r) => r.version === rightV);
    if (!left || !right) {
      revDiff.innerHTML = "<p>Keine Revisionen vorhanden.</p>";
      return;
    }

    revRestoreBtn.textContent = `Version ${left.version} wiederherstellen`;
    revRestoreBtn.disabled = left.version === activeRevisions[0].version;

    if (left.version === right.version) {
      revDiff.innerHTML = `
        <div class="diff-field">
          <div class="diff-field-title">Inhalt von Version ${left.version}</div>
          <div class="line-diff-body"><div class="diff-line-same">${escapeHTML(left.title)}\n\n${escapeHTML(left.error_codes || "")}\n\n${escapeHTML(left.symptoms || "")}\n\n${escapeHTML(left.solution_body || "")}</div></div>
        </div>
      `;
      return;
    }

    const html = [
      renderDiffBlock("Titel", left.title, right.title),
      renderDiffBlock("Fehlercodes", left.error_codes, right.error_codes),
      renderDiffBlock("Status", left.status, right.status),
      renderDiffBlock("Tags", (left.tags || []).join(", "), (right.tags || []).join(", ")),
      renderDiffBlock("Symptome / Ursache", left.symptoms, right.symptoms),
      renderDiffBlock("Lösung / Inhalt", left.solution_body, right.solution_body),
    ].filter(Boolean).join("");

    revDiff.innerHTML = html || "<p class=\"hit-snippet\">Keine inhaltlichen Unterschiede zwischen diesen beiden Versionen.</p>";
  }

  revSelLeft.addEventListener("change", renderRevisionDiff);
  revSelRight.addEventListener("change", renderRevisionDiff);
  revCloseBtn.addEventListener("click", () => revDialog.close());

  revRestoreBtn.addEventListener("click", async () => {
    const targetV = Number(revSelLeft.value);
    if (!targetV || !activeArticleId) return;
    if (!confirm(`Stand aus Version ${targetV} als neue Revision wiederherstellen?`)) return;

    const { ok, body } = await apiRequest(`/api/articles/${activeArticleId}/revisions/${targetV}/restore`, {
      method: "POST",
      body: JSON.stringify({ editor_name: getAuthor() }),
    });
    if (!ok || !body?.article) {
      showToast(body?.error || "Wiederherstellen fehlgeschlagen");
      return;
    }
    revDialog.close();
    showToast(`Version ${targetV} wiederhergestellt (neu: v${body.article.version})`);
    onRestoredArticle(body.article.id);
  });

  // --- Verwaltungs-Panel ---
  const synList = document.getElementById("admin-syn-list");
  const synInput = document.getElementById("admin-syn-input");
  const synAddBtn = document.getElementById("admin-syn-add");

  const catList = document.getElementById("admin-cat-list");
  const catName = document.getElementById("admin-cat-name");
  const catDesc = document.getElementById("admin-cat-desc");
  const catOrder = document.getElementById("admin-cat-order");
  const catAddBtn = document.getElementById("admin-cat-add");

  const setTitleInput = document.getElementById("admin-site-title");
  const setDaysInput = document.getElementById("admin-reminder-days");
  const setSaveBtn = document.getElementById("admin-settings-save");
  const adminPwInput = document.getElementById("admin-password-input");
  const adminPwWrap = document.getElementById("admin-password-wrap");

  const orphanSummary = document.getElementById("admin-orphan-summary");
  const btnCheckOrphans = document.getElementById("btn-check-orphans");
  const btnCleanOrphans = document.getElementById("btn-clean-orphans");
  const btnEmptyTrash = document.getElementById("btn-empty-trash");

  function adminHeaders() {
    const pw = (adminPwInput.value || "").trim();
    return pw ? { "X-Admin-Password": pw } : {};
  }

  async function refreshAdminView() {
    const meta = getMeta();
    setTitleInput.value = meta.site_title || "";
    setDaysInput.value = String(meta.review_reminder_days ?? 180);
    adminPwWrap.classList.toggle("hidden", !meta.admin_protected);

    await Promise.all([loadSynonymsUI(), loadCategoriesUI()]);
  }

  async function loadSynonymsUI() {
    const { ok, body } = await apiRequest("/api/synonyms");
    if (!ok || !Array.isArray(body)) return;
    synList.innerHTML = "";
    for (const g of body) {
      const row = document.createElement("div");
      row.className = "admin-row";
      const inp = document.createElement("input");
      inp.type = "text";
      inp.value = (g.terms || []).join(", ");
      inp.className = "admin-row-input";

      const saveBtn = document.createElement("button");
      saveBtn.type = "button";
      saveBtn.className = "btn";
      saveBtn.textContent = "Speichern";
      saveBtn.addEventListener("click", async () => {
        const terms = inp.value.split(",").map((s) => s.trim()).filter(Boolean);
        const res = await apiRequest(`/api/synonyms/${g.id}`, {
          method: "PUT",
          body: JSON.stringify({ terms }),
        });
        if (!res.ok) {
          showToast(res.body?.error || "Fehler beim Speichern");
          return;
        }
        showToast("Synonym-Gruppe aktualisiert");
        loadSynonymsUI();
      });

      const delBtn = document.createElement("button");
      delBtn.type = "button";
      delBtn.className = "btn btn-danger";
      delBtn.textContent = "Löschen";
      delBtn.addEventListener("click", async () => {
        const res = await apiRequest(`/api/synonyms/${g.id}`, { method: "DELETE" });
        if (res.ok) {
          showToast("Synonym-Gruppe gelöscht");
          loadSynonymsUI();
        }
      });

      row.append(inp, saveBtn, delBtn);
      synList.appendChild(row);
    }
  }

  synAddBtn.addEventListener("click", async () => {
    const terms = synInput.value.split(",").map((s) => s.trim()).filter(Boolean);
    const res = await apiRequest("/api/synonyms", {
      method: "POST",
      body: JSON.stringify({ terms }),
    });
    if (!res.ok) {
      showToast(res.body?.error || "Mindestens 2 Begriffe kommagetrennt angeben");
      return;
    }
    synInput.value = "";
    showToast("Synonym-Gruppe hinzugefügt");
    loadSynonymsUI();
  });

  async function loadCategoriesUI() {
    const { ok, body } = await apiRequest("/api/categories");
    if (!ok || !Array.isArray(body)) return;
    catList.innerHTML = "";
    for (const c of body) {
      const row = document.createElement("div");
      row.className = "admin-row";
      const nInp = document.createElement("input");
      nInp.type = "text";
      nInp.value = c.name;
      nInp.placeholder = "Name";

      const dInp = document.createElement("input");
      dInp.type = "text";
      dInp.value = c.description || "";
      dInp.placeholder = "Beschreibung";
      dInp.className = "admin-row-input";

      const oInp = document.createElement("input");
      oInp.type = "number";
      oInp.value = String(c.sort_order ?? 0);
      oInp.className = "admin-order-input";

      const saveBtn = document.createElement("button");
      saveBtn.type = "button";
      saveBtn.className = "btn";
      saveBtn.textContent = "Speichern";
      saveBtn.addEventListener("click", async () => {
        const res = await apiRequest(`/api/categories/${c.id}`, {
          method: "PUT",
          body: JSON.stringify({ name: nInp.value, description: dInp.value, sort_order: Number(oInp.value) || 0 }),
        });
        if (!res.ok) {
          showToast(res.body?.error || "Fehler beim Speichern");
          return;
        }
        showToast("Kategorie aktualisiert");
        await reloadMeta();
        loadCategoriesUI();
      });

      const delBtn = document.createElement("button");
      delBtn.type = "button";
      delBtn.className = "btn btn-danger";
      delBtn.textContent = "Löschen";
      delBtn.addEventListener("click", async () => {
        if (!confirm(`Kategorie „${c.name}“ löschen? Artikel bleiben ohne Kategorie erhalten.`)) return;
        const res = await apiRequest(`/api/categories/${c.id}`, { method: "DELETE" });
        if (res.ok) {
          showToast("Kategorie gelöscht");
          await reloadMeta();
          loadCategoriesUI();
        }
      });

      row.append(nInp, dInp, oInp, saveBtn, delBtn);
      catList.appendChild(row);
    }
  }

  catAddBtn.addEventListener("click", async () => {
    const res = await apiRequest("/api/categories", {
      method: "POST",
      body: JSON.stringify({
        name: catName.value,
        description: catDesc.value,
        sort_order: Number(catOrder.value) || 50,
      }),
    });
    if (!res.ok) {
      showToast(res.body?.error || "Kategorie konnte nicht angelegt werden");
      return;
    }
    catName.value = "";
    catDesc.value = "";
    showToast("Kategorie angelegt");
    await reloadMeta();
    loadCategoriesUI();
  });

  setSaveBtn.addEventListener("click", async () => {
    const res = await apiRequest("/api/settings", {
      method: "PUT",
      headers: adminHeaders(),
      body: JSON.stringify({
        site_title: setTitleInput.value.trim(),
        review_reminder_days: Number(setDaysInput.value) || 0,
      }),
    });
    if (!res.ok) {
      showToast(res.body?.error || "Einstellungen konnten nicht gespeichert werden");
      return;
    }
    await reloadMeta();
    showToast("Einstellungen gespeichert");
  });

  btnCheckOrphans.addEventListener("click", async () => {
    const res = await apiRequest("/api/media/orphans", { headers: adminHeaders() });
    if (!res.ok) {
      showToast(res.body?.error || "Prüfung fehlgeschlagen (Admin-Passwort prüfen)");
      return;
    }
    const kb = Math.round((res.body.total_bytes || 0) / 1024);
    orphanSummary.textContent = `${res.body.count} verwaiste Datei(en) gefunden (${kb} KB).`;
    btnCleanOrphans.disabled = res.body.count === 0;
  });

  btnCleanOrphans.addEventListener("click", async () => {
    if (!confirm("Alle unreferenzierten Mediendateien endgültig von der Festplatte entfernen?")) return;
    const res = await apiRequest("/api/media/cleanup", { method: "POST", headers: adminHeaders() });
    if (!res.ok) {
      showToast(res.body?.error || "Bereinigung fehlgeschlagen");
      return;
    }
    const kb = Math.round((res.body.freed_bytes || 0) / 1024);
    orphanSummary.textContent = `${res.body.deleted} Datei(en) entfernt (${kb} KB freigegeben).`;
    btnCleanOrphans.disabled = true;
    showToast("Verwaiste Medien bereinigt");
  });

  btnEmptyTrash.addEventListener("click", async () => {
    if (!confirm("Alle Artikel im Papierkorb endgültig löschen?")) return;
    const res = await apiRequest("/api/trash", { method: "DELETE", headers: adminHeaders() });
    if (!res.ok) {
      showToast(res.body?.error || "Papierkorb konnte nicht geleert werden");
      return;
    }
    showToast(`Papierkorb geleert (${res.body.deleted} Artikel entfernt)`);
  });

  // Tab-Umschaltung im Verwaltungs-Panel
  for (const tabBtn of document.querySelectorAll("[data-admin-tab]")) {
    tabBtn.addEventListener("click", () => {
      document.querySelectorAll("[data-admin-tab]").forEach((b) => b.classList.remove("active"));
      tabBtn.classList.add("active");
      const target = tabBtn.dataset.adminTab;
      document.querySelectorAll(".admin-tab-panel").forEach((p) => {
        p.classList.toggle("hidden", p.id !== `admin-tab-${target}`);
      });
    });
  }

  return { openHistory, refreshAdminView, adminHeaders };
}
