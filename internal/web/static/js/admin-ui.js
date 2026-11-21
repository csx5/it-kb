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

export function createAdminAndHistoryController({ apiRequest, getAuthor, getMeta, getUser, canEdit, isAdmin, reloadMeta, onRestoredArticle, showToast }) {
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
    revRestoreBtn.classList.toggle("hidden", !canEdit());
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

  // --- Verwaltungs-Panel & Admin-Konsole ---
  const usersList = document.getElementById("admin-users-list");
  const newUserName = document.getElementById("new-user-name");
  const newUserDisplay = document.getElementById("new-user-display");
  const newUserRole = document.getElementById("new-user-role");
  const newUserPassword = document.getElementById("new-user-password");
  const btnCreateUser = document.getElementById("btn-create-user");
  const auditBody = document.getElementById("admin-audit-body");
  const btnRefreshAudit = document.getElementById("btn-refresh-audit");

  const tabBtnUsers = document.getElementById("tab-btn-users");
  const tabBtnAudit = document.getElementById("tab-btn-audit");
  const tabBtnMaintenance = document.getElementById("tab-btn-maintenance");

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
  const btnBackupFull = document.getElementById("btn-backup-full");
  const btnRestoreBackup = document.getElementById("btn-restore-backup");
  const restoreBackupInput = document.getElementById("restore-backup-input");
  const backupPassphraseInput = document.getElementById("backup-passphrase");

  function adminHeaders() {
    const pw = (adminPwInput.value || "").trim();
    const h = { "X-Requested-With": "it-kb" };
    if (pw) h["X-Admin-Password"] = pw;
    return h;
  }

  function activateAdminTab(target) {
    document.querySelectorAll("[data-admin-tab]").forEach((b) => {
      b.classList.toggle("active", b.dataset.adminTab === target);
    });
    document.querySelectorAll(".admin-tab-panel").forEach((p) => {
      p.classList.toggle("hidden", p.id !== `admin-tab-${target}`);
    });
  }

  async function refreshAdminView() {
    const meta = getMeta();
    const adminAllowed = isAdmin();
    const authOn = Boolean(meta.auth_required);

    setTitleInput.value = meta.site_title || "";
    setDaysInput.value = String(meta.review_reminder_days ?? 180);
    adminPwWrap.classList.toggle("hidden", !meta.admin_protected);

    tabBtnUsers.classList.toggle("hidden", !(authOn && adminAllowed));
    tabBtnAudit.classList.toggle("hidden", !(authOn && adminAllowed));
    tabBtnMaintenance.classList.toggle("hidden", !adminAllowed && authOn);

    if (authOn && adminAllowed) {
      activateAdminTab("users");
      await Promise.all([loadUsersUI(), loadAuditUI(), loadSynonymsUI(), loadCategoriesUI()]);
    } else {
      activateAdminTab("synonyms");
      await Promise.all([loadSynonymsUI(), loadCategoriesUI()]);
    }
  }

  async function loadUsersUI() {
    const { ok, body } = await apiRequest("/api/admin/users");
    if (!ok || !body?.users) return;
    const me = getUser();
    usersList.innerHTML = "";

    for (const u of body.users) {
      const row = document.createElement("div");
      row.className = "admin-row";

      const uname = document.createElement("span");
      uname.className = "badge badge-code";
      uname.textContent = u.username;

      const disp = document.createElement("input");
      disp.type = "text";
      disp.value = u.display_name || "";
      disp.placeholder = "Anzeigename";
      disp.className = "admin-row-input";

      const roleSel = document.createElement("select");
      roleSel.innerHTML = `
        <option value="admin">Administrator</option>
        <option value="editor">Bearbeiter</option>
        <option value="reader">Leser</option>
      `;
      roleSel.value = u.role;

      const activeLabel = document.createElement("label");
      activeLabel.className = "checkbox-inline";
      const activeChk = document.createElement("input");
      activeChk.type = "checkbox";
      activeChk.checked = Boolean(u.is_active);
      activeLabel.append(activeChk, document.createTextNode("Aktiv"));

      const saveBtn = document.createElement("button");
      saveBtn.type = "button";
      saveBtn.className = "btn btn-sm";
      saveBtn.textContent = "Speichern";
      saveBtn.addEventListener("click", async () => {
        const res = await apiRequest(`/api/admin/users/${u.id}`, {
          method: "PUT",
          body: JSON.stringify({
            display_name: disp.value.trim(),
            role: roleSel.value,
            is_active: activeChk.checked,
          }),
        });
        if (!res.ok) {
          showToast(res.body?.error || "Speichern fehlgeschlagen");
          return;
        }
        showToast(`Benutzer „${u.username}“ aktualisiert`);
        loadUsersUI();
        loadAuditUI();
      });

      const resetBtn = document.createElement("button");
      resetBtn.type = "button";
      resetBtn.className = "btn btn-sm";
      resetBtn.textContent = "Passwort";
      resetBtn.addEventListener("click", async () => {
        const newPw = prompt(`Neues Passwort für „${u.username}“ (mindestens 10 Zeichen):`);
        if (!newPw) return;
        const res = await apiRequest(`/api/admin/users/${u.id}/password`, {
          method: "POST",
          body: JSON.stringify({ new_password: newPw }),
        });
        if (!res.ok) {
          showToast(res.body?.error || "Passwort-Reset fehlgeschlagen");
          return;
        }
        showToast(`Passwort für „${u.username}“ zurückgesetzt`);
        loadAuditUI();
      });

      const totpBtn = document.createElement("button");
      totpBtn.type = "button";
      totpBtn.className = "btn btn-sm";
      totpBtn.textContent = u.totp_enabled ? "2FA zurücksetzen" : "2FA aus";
      totpBtn.disabled = !u.totp_enabled;
      totpBtn.addEventListener("click", async () => {
        if (!confirm(`2FA (TOTP) für „${u.username}“ zurücksetzen?`)) return;
        const res = await apiRequest(`/api/admin/users/${u.id}/totp-reset`, { method: "POST" });
        if (!res.ok) {
          showToast(res.body?.error || "2FA-Reset fehlgeschlagen");
          return;
        }
        showToast(`2FA für „${u.username}“ zurückgesetzt`);
        loadUsersUI();
        loadAuditUI();
      });

      const delBtn = document.createElement("button");
      delBtn.type = "button";
      delBtn.className = "btn btn-sm btn-danger";
      delBtn.textContent = "Löschen";
      delBtn.disabled = Boolean(me && me.id === u.id);
      delBtn.addEventListener("click", async () => {
        if (!confirm(`Benutzerkonto „${u.username}“ wirklich dauerhaft löschen?`)) return;
        const res = await apiRequest(`/api/admin/users/${u.id}`, { method: "DELETE" });
        if (!res.ok) {
          showToast(res.body?.error || "Löschen fehlgeschlagen");
          return;
        }
        showToast(`Benutzer „${u.username}“ gelöscht`);
        loadUsersUI();
        loadAuditUI();
      });

      row.append(uname, disp, roleSel, activeLabel, saveBtn, resetBtn, totpBtn, delBtn);
      usersList.appendChild(row);
    }
  }

  btnCreateUser.addEventListener("click", async () => {
    const res = await apiRequest("/api/admin/users", {
      method: "POST",
      body: JSON.stringify({
        username: newUserName.value.trim(),
        display_name: newUserDisplay.value.trim(),
        role: newUserRole.value,
        password: newUserPassword.value,
      }),
    });
    if (!res.ok) {
      showToast(res.body?.error || "Benutzer konnte nicht angelegt werden");
      return;
    }
    newUserName.value = "";
    newUserDisplay.value = "";
    newUserPassword.value = "";
    showToast(`Benutzer „${res.body.user.username}“ angelegt`);
    loadUsersUI();
    loadAuditUI();
  });

  async function loadAuditUI() {
    const { ok, body } = await apiRequest("/api/admin/audit");
    if (!ok || !body?.entries) return;
    auditBody.innerHTML = "";
    for (const e of body.entries) {
      const tr = document.createElement("tr");
      const timeStr = new Date(e.created_at).toLocaleString("de-DE");
      tr.innerHTML = `
        <td>${escapeHTML(timeStr)}</td>
        <td>${escapeHTML(e.actor_name || "–")}</td>
        <td><code>${escapeHTML(e.action)}</code></td>
        <td>${escapeHTML(e.target || "–")}</td>
        <td><code>${escapeHTML(e.ip_address || "–")}</code></td>
        <td>${escapeHTML(e.details || "")}</td>
      `;
      auditBody.appendChild(tr);
    }
  }

  btnRefreshAudit.addEventListener("click", loadAuditUI);

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

  btnBackupFull.addEventListener("click", async (e) => {
    e.preventDefault();
    try {
      const pass = (backupPassphraseInput.value || "").trim();
      const headers = adminHeaders();
      if (pass) {
        if (pass.length < 8) {
          showToast("Backup-Passphrase muss mindestens 8 Zeichen lang sein");
          return;
        }
        headers["X-Backup-Passphrase"] = pass;
      }
      showToast(pass ? "Verschlüsseltes Voll-Backup wird erstellt …" : "Voll-Backup wird erstellt …");
      const res = await fetch("/api/backup/full", { headers });
      if (!res.ok) {
        let msg = `Backup fehlgeschlagen (HTTP ${res.status})`;
        try {
          const j = await res.json();
          if (j?.error) msg = j.error;
        } catch {}
        showToast(msg);
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-");
      a.download = pass ? `it-kb-vollbackup-${stamp}.itkb.enc` : `it-kb-vollbackup-${stamp}.zip`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
      showToast(pass ? "Verschlüsseltes Voll-Backup heruntergeladen" : "Voll-Backup heruntergeladen");
    } catch {
      showToast("Voll-Backup fehlgeschlagen");
    }
  });

  btnRestoreBackup.addEventListener("click", () => restoreBackupInput.click());
  restoreBackupInput.addEventListener("change", async () => {
    const file = restoreBackupInput.files?.[0];
    restoreBackupInput.value = "";
    if (!file) return;
    if (!confirm(`Voll-Backup „${file.name}“ wirklich einspielen? Der aktuelle Datenbank- und Medienstand wird vollständig ersetzt.`)) {
      return;
    }
    let pass = (backupPassphraseInput.value || "").trim();
    if (!pass && file.name.endsWith(".enc")) {
      pass = prompt("Backup-Passphrase zum Entschlüsseln eingeben:") || "";
    }
    const fd = new FormData();
    fd.append("file", file, file.name);
    if (pass) fd.append("passphrase", pass);
    showToast("Backup wird wiederhergestellt …");
    const res = await fetch("/api/backup/restore", {
      method: "POST",
      headers: adminHeaders(),
      body: fd,
    });
    let body = null;
    try {
      body = await res.json();
    } catch {}
    if (!res.ok) {
      showToast(body?.error || `Restore fehlgeschlagen (HTTP ${res.status})`);
      return;
    }
    await reloadMeta();
    await refreshAdminView();
    showToast("Voll-Backup erfolgreich wiederhergestellt");
  });

  // Tab-Umschaltung im Verwaltungs-Panel
  for (const tabBtn of document.querySelectorAll("[data-admin-tab]")) {
    tabBtn.addEventListener("click", () => {
      const target = tabBtn.dataset.adminTab;
      activateAdminTab(target);
      if (target === "audit") loadAuditUI();
      if (target === "users") loadUsersUI();
    });
  }

  return { openHistory, refreshAdminView, adminHeaders };
}
