import { createImageEditorController } from "/js/editor-canvas.js";
import { createAdminAndHistoryController } from "/js/admin-ui.js";

const THEME_KEY = "itkb_theme";
const AUTHOR_KEY = "itkb_author";

const imageEditor = createImageEditorController();

const els = {
  siteTitle: document.getElementById("site-title"),
  appVersion: document.getElementById("app-version"),
  btnNewArticle: document.getElementById("btn-new-article"),
  btnAdminView: document.getElementById("btn-admin-view"),
  adminPanel: document.getElementById("admin-panel"),
  btnAdminClose: document.getElementById("btn-admin-close"),
  authorInput: document.getElementById("author-input"),
  themeToggle: document.getElementById("theme-toggle"),
  searchPanel: document.getElementById("search-panel"),
  searchInput: document.getElementById("search-input"),
  filterCategory: document.getElementById("filter-category"),
  filterStatus: document.getElementById("filter-status"),
  filterTemplate: document.getElementById("filter-template"),
  filterNeedsReview: document.getElementById("filter-needs-review"),
  filterDeleted: document.getElementById("filter-deleted"),
  resultMeta: document.getElementById("result-meta"),
  resultsList: document.getElementById("results-list"),
  articleDetail: document.getElementById("article-detail"),
  articleEditor: document.getElementById("article-editor"),
  editorHeading: document.getElementById("editor-heading"),
  editorLockBanner: document.getElementById("editor-lock-banner"),
  editorPanes: document.getElementById("editor-panes"),
  btnEditorCancel: document.getElementById("btn-editor-cancel"),
  btnEditorSave: document.getElementById("btn-editor-save"),
  edTitle: document.getElementById("ed-title"),
  edTemplate: document.getElementById("ed-template"),
  edCategory: document.getElementById("ed-category"),
  edStatus: document.getElementById("ed-status"),
  edTags: document.getElementById("ed-tags"),
  edErrorCodesWrap: document.getElementById("ed-error-codes-wrap"),
  edErrorCodes: document.getElementById("ed-error-codes"),
  edSymptomsWrap: document.getElementById("ed-symptoms-wrap"),
  edSymptoms: document.getElementById("ed-symptoms"),
  edSolutionLabel: document.getElementById("ed-solution-label"),
  edSolution: document.getElementById("ed-solution"),
  edSummaryWrap: document.getElementById("ed-summary-wrap"),
  edSummary: document.getElementById("ed-summary"),
  edPreviewContent: document.getElementById("ed-preview-content"),
  conflictDialog: document.getElementById("conflict-dialog"),
  conflictIntro: document.getElementById("conflict-intro"),
  conflictDiff: document.getElementById("conflict-diff"),
  btnConflictCancel: document.getElementById("btn-conflict-cancel"),
  btnConflictTakeServer: document.getElementById("btn-conflict-take-server"),
  btnConflictForce: document.getElementById("btn-conflict-force"),
  btnUploadMedia: document.getElementById("btn-upload-media"),
  mediaFileInput: document.getElementById("media-file-input"),
  lightboxDialog: document.getElementById("lightbox-dialog"),
  lightboxImg: document.getElementById("lightbox-img"),
  lightboxClose: document.getElementById("lightbox-close"),
  toast: document.getElementById("toast"),
};

const state = {
  meta: { categories: [], admin_protected: false },
  currentArticle: null,
  editingArticleId: null,
  editingVersion: 0,
  lastFocusedTextarea: null,
  lockInterval: null,
  conflictServerArticle: null,
};

let searchTimer = null;
let previewTimer = null;
let toastTimer = null;

function getAuthor() {
  return (els.authorInput.value || "").trim() || "Anonym";
}

function showToast(message) {
  els.toast.textContent = message;
  els.toast.classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => els.toast.classList.add("hidden"), 3000);
}

function initTheme() {
  const saved = localStorage.getItem(THEME_KEY) || "auto";
  document.documentElement.setAttribute("data-theme", saved);
  els.themeToggle.addEventListener("click", () => {
    const cur = document.documentElement.getAttribute("data-theme");
    const next = cur === "dark" ? "light" : "dark";
    document.documentElement.setAttribute("data-theme", next);
    localStorage.setItem(THEME_KEY, next);
  });
}

function initAuthor() {
  els.authorInput.value = localStorage.getItem(AUTHOR_KEY) || "";
  els.authorInput.addEventListener("input", () => {
    localStorage.setItem(AUTHOR_KEY, els.authorInput.value.trim());
  });
}

async function apiRequest(url, options = {}) {
  const res = await fetch(url, {
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    body = null;
  }
  return { ok: res.ok, status: res.status, body };
}

function showView(viewName) {
  els.resultsList.classList.toggle("hidden", viewName !== "list");
  els.articleDetail.classList.toggle("hidden", viewName !== "detail");
  els.articleEditor.classList.toggle("hidden", viewName !== "editor");
  els.adminPanel.classList.toggle("hidden", viewName !== "admin");
  els.searchPanel.classList.toggle("hidden", viewName === "editor" || viewName === "admin");

  if (viewName !== "editor") {
    stopLockHeartbeat();
  }
}

async function loadMeta() {
  const { ok, body } = await apiRequest("/api/meta");
  if (!ok || !body) throw new Error("Metadaten konnten nicht geladen werden");
  state.meta = body;
  els.siteTitle.textContent = body.site_title || "IT-Wissensdatenbank";
  document.title = els.siteTitle.textContent;
  els.appVersion.textContent = body.version || "dev";

  els.filterCategory.innerHTML = `<option value="">Alle Kategorien</option>`;
  els.edCategory.innerHTML = `<option value="">Ohne Kategorie</option>`;
  for (const cat of body.categories || []) {
    const o1 = document.createElement("option");
    o1.value = String(cat.id);
    o1.textContent = cat.name;
    els.filterCategory.appendChild(o1);

    const o2 = document.createElement("option");
    o2.value = String(cat.id);
    o2.textContent = cat.name;
    els.edCategory.appendChild(o2);
  }
}

async function runSearch() {
  const params = new URLSearchParams();
  if (els.searchInput.value.trim()) params.set("q", els.searchInput.value.trim());
  if (els.filterCategory.value) params.set("category_id", els.filterCategory.value);
  if (els.filterStatus.value) params.set("status", els.filterStatus.value);
  if (els.filterTemplate.value) params.set("template", els.filterTemplate.value);
  if (els.filterNeedsReview.checked) params.set("needs_review", "true");
  if (els.filterDeleted.checked) params.set("deleted", "true");

  const { ok, body } = await apiRequest(`/api/search?${params.toString()}`);
  if (!ok || !body) {
    showToast("Suche fehlgeschlagen");
    return;
  }
  const suffix = els.filterDeleted.checked ? " im Papierkorb" : "";
  els.resultMeta.textContent = `${body.count} Treffer${suffix}`;
  renderResults(body.hits || []);
}

function renderResults(hits) {
  els.resultsList.innerHTML = "";
  if (hits.length === 0) {
    const empty = document.createElement("p");
    empty.className = "hit-snippet";
    empty.textContent = els.filterDeleted.checked
      ? "Der Papierkorb ist leer."
      : "Keine passenden Einträge gefunden.";
    els.resultsList.appendChild(empty);
    return;
  }

  for (const hit of hits) {
    const a = hit.article;
    const card = document.createElement("div");
    card.className = "hit-card";
    card.addEventListener("click", () => openArticle(a.id));

    const header = document.createElement("div");
    header.className = "hit-header";

    const title = document.createElement("h3");
    title.className = "hit-title";
    title.textContent = a.title;
    header.appendChild(title);

    const catBadge = document.createElement("span");
    catBadge.className = "badge";
    catBadge.textContent = a.category_name || "Ohne Kategorie";
    header.appendChild(catBadge);
    card.appendChild(header);

    const badges = document.createElement("div");
    badges.className = "hit-badges";
    if (a.deleted_at) {
      badges.appendChild(makeBadge("Papierkorb", "badge-danger"));
    }
    if (a.status === "draft") {
      badges.appendChild(makeBadge("Entwurf", "badge-warn"));
    } else if (a.status === "outdated") {
      badges.appendChild(makeBadge("Veraltet", "badge-danger"));
    }
    if (a.error_codes) {
      badges.appendChild(makeBadge(a.error_codes, "badge-code"));
    }
    if (hit.needs_review) {
      badges.appendChild(makeBadge("Prüfung fällig", "badge-warn"));
    }
    for (const tag of a.tags || []) {
      badges.appendChild(makeBadge(`#${tag}`, ""));
    }
    card.appendChild(badges);

    const snippet = document.createElement("p");
    snippet.className = "hit-snippet";
    snippet.innerHTML = hit.snippet_html || "";
    card.appendChild(snippet);

    els.resultsList.appendChild(card);
  }
}

function makeBadge(text, extraClass) {
  const el = document.createElement("span");
  el.className = `badge ${extraClass}`.trim();
  el.textContent = text;
  return el;
}

async function openArticle(id) {
  const { ok, body } = await apiRequest(`/api/articles/${id}`);
  if (!ok || !body) {
    showToast("Artikel konnte nicht geladen werden");
    return;
  }
  const a = body.article;
  state.currentArticle = a;
  showView("detail");

  els.articleDetail.innerHTML = "";

  const topbar = document.createElement("div");
  topbar.className = "detail-topbar";

  const backBtn = document.createElement("button");
  backBtn.type = "button";
  backBtn.className = "btn";
  backBtn.textContent = "Zurück zur Übersicht";
  backBtn.addEventListener("click", () => {
    showView("list");
    runSearch();
  });

  const actions = document.createElement("div");
  actions.className = "detail-actions";

  if (!a.deleted_at) {
    const historyBtn = document.createElement("button");
    historyBtn.type = "button";
    historyBtn.className = "btn";
    historyBtn.textContent = `Historie (v${a.version})`;
    historyBtn.addEventListener("click", () => adminUI.openHistory(a.id));

    const verifyBtn = document.createElement("button");
    verifyBtn.type = "button";
    verifyBtn.className = "btn";
    verifyBtn.textContent = "Als geprüft markieren";
    verifyBtn.addEventListener("click", () => verifyArticle(a.id));

    const editBtn = document.createElement("button");
    editBtn.type = "button";
    editBtn.className = "btn btn-primary";
    editBtn.textContent = "Bearbeiten";
    editBtn.addEventListener("click", () => openEditor(a));

    const delBtn = document.createElement("button");
    delBtn.type = "button";
    delBtn.className = "btn btn-danger";
    delBtn.textContent = "In Papierkorb";
    delBtn.addEventListener("click", () => softDeleteArticle(a.id));

    actions.append(historyBtn, verifyBtn, editBtn, delBtn);
  } else {
    const restoreBtn = document.createElement("button");
    restoreBtn.type = "button";
    restoreBtn.className = "btn btn-primary";
    restoreBtn.textContent = "Wiederherstellen";
    restoreBtn.addEventListener("click", () => restoreArticle(a.id));

    const permBtn = document.createElement("button");
    permBtn.type = "button";
    permBtn.className = "btn btn-danger";
    permBtn.textContent = "Endgültig löschen";
    permBtn.addEventListener("click", () => permanentDeleteArticle(a.id));

    actions.append(restoreBtn, permBtn);
  }

  topbar.append(backBtn, actions);
  els.articleDetail.appendChild(topbar);

  if (body.active_lock && body.active_lock.editor) {
    const warn = document.createElement("div");
    warn.className = "banner banner-warn";
    warn.textContent = `Hinweis: Dieser Artikel wird gerade von ${body.active_lock.editor} bearbeitet.`;
    els.articleDetail.appendChild(warn);
  }

  const heading = document.createElement("h2");
  heading.textContent = a.title;
  els.articleDetail.appendChild(heading);

  const metaLine = document.createElement("div");
  metaLine.className = "hit-badges";
  metaLine.appendChild(makeBadge(a.category_name || "Ohne Kategorie", ""));
  metaLine.appendChild(makeBadge(`Status: ${formatStatus(a.status)}`, ""));
  metaLine.appendChild(makeBadge(`Version ${a.version}`, ""));
  metaLine.appendChild(makeBadge(`Autor: ${a.author_name || "Unbekannt"}`, ""));
  if (a.last_verified_at) {
    const d = new Date(a.last_verified_at).toLocaleDateString("de-DE");
    metaLine.appendChild(makeBadge(`Geprüft am ${d} (${a.last_verified_by || "–"})`, "badge-ok"));
  }
  for (const tag of a.tags || []) {
    metaLine.appendChild(makeBadge(`#${tag}`, ""));
  }
  els.articleDetail.appendChild(metaLine);

  if (a.error_codes) {
    const lbl = document.createElement("div");
    lbl.className = "detail-section-label";
    lbl.textContent = "Fehlercode / Fehlermeldung";
    const box = document.createElement("pre");
    box.textContent = a.error_codes;
    els.articleDetail.append(lbl, box);
  }

  if (body.symptoms_html) {
    const lbl = document.createElement("div");
    lbl.className = "detail-section-label";
    lbl.textContent = "Symptome / Ursache";
    const div = document.createElement("div");
    div.innerHTML = body.symptoms_html;
    els.articleDetail.append(lbl, div);
  }

  const solLbl = document.createElement("div");
  solLbl.className = "detail-section-label";
  solLbl.textContent = a.template_type === "howto" ? "Anleitung" : "Lösung";
  const solDiv = document.createElement("div");
  solDiv.innerHTML = body.solution_html || "";
  els.articleDetail.append(solLbl, solDiv);

  attachCodeCopyButtons(els.articleDetail);
  attachImageLightbox(els.articleDetail);
}

function attachImageLightbox(container) {
  for (const img of container.querySelectorAll("img")) {
    img.addEventListener("click", () => {
      els.lightboxImg.src = img.src;
      els.lightboxImg.alt = img.alt || "Screenshot";
      els.lightboxDialog.showModal();
    });
  }
}

async function uploadMediaBlob(blob, filename) {
  const fd = new FormData();
  fd.append("file", blob, filename);
  fd.append("uploaded_by", getAuthor());

  const res = await fetch("/api/media", { method: "POST", body: fd });
  let body = null;
  try {
    body = await res.json();
  } catch {
    body = null;
  }
  if (!res.ok || !body?.markdown) {
    throw new Error(body?.error || `Upload fehlgeschlagen (HTTP ${res.status})`);
  }
  return body;
}

function insertAtCursor(textarea, snippet) {
  const ta = textarea || state.lastFocusedTextarea || els.edSolution;
  const start = ta.selectionStart ?? ta.value.length;
  const end = ta.selectionEnd ?? ta.value.length;
  const prefix = start > 0 && ta.value[start - 1] !== "\n" ? "\n" : "";
  const suffix = "\n";
  ta.setRangeText(prefix + snippet + suffix, start, end, "end");
  ta.focus();
  schedulePreview();
}

async function handleImageWithEditor(fileOrBlob, suggestedName, targetTextarea) {
  const edited = await imageEditor.open(fileOrBlob, suggestedName);
  if (!edited) return;
  try {
    showToast("Screenshot wird hochgeladen …");
    const uploaded = await uploadMediaBlob(edited.blob, edited.filename);
    insertAtCursor(targetTextarea, uploaded.markdown);
    showToast("Screenshot eingefügt");
  } catch (err) {
    showToast(err.message || "Upload fehlgeschlagen");
  }
}

async function handleFilesUpload(files, targetTextarea) {
  const list = [...files];
  if (list.length === 0) return;

  // Einzelnes Bild direkt über den Screenshot-Editor führen (Schwärzen/Markieren + Komprimierung)
  if (list.length === 1 && list[0].type.startsWith("image/")) {
    await handleImageWithEditor(list[0], list[0].name || "screenshot", targetTextarea);
    return;
  }

  for (const file of list) {
    try {
      showToast(`Lade hoch: ${file.name} …`);
      const uploaded = await uploadMediaBlob(file, file.name);
      insertAtCursor(targetTextarea, uploaded.markdown);
    } catch (err) {
      showToast(`${file.name}: ${err.message}`);
    }
  }
  showToast("Upload abgeschlossen");
}

function bindMediaInputsToTextarea(ta) {
  ta.addEventListener("paste", async (e) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    for (const item of items) {
      if (item.type.startsWith("image/")) {
        e.preventDefault();
        const blob = item.getAsFile();
        if (blob) {
          const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-");
          await handleImageWithEditor(blob, `screenshot-${stamp}`, ta);
        }
        return;
      }
    }
  });

  ta.addEventListener("dragover", (e) => {
    e.preventDefault();
    ta.classList.add("drop-active");
  });
  ta.addEventListener("dragleave", () => ta.classList.remove("drop-active"));
  ta.addEventListener("drop", async (e) => {
    e.preventDefault();
    ta.classList.remove("drop-active");
    if (e.dataTransfer?.files?.length) {
      await handleFilesUpload(e.dataTransfer.files, ta);
    }
  });
}

async function verifyArticle(id) {
  const { ok } = await apiRequest(`/api/articles/${id}/verify`, {
    method: "POST",
    body: JSON.stringify({ verifier: getAuthor() }),
  });
  if (!ok) {
    showToast("Konnte nicht als geprüft markiert werden");
    return;
  }
  showToast("Artikel als geprüft markiert");
  await openArticle(id);
}

async function softDeleteArticle(id) {
  if (!confirm("Artikel in den Papierkorb verschieben?")) return;
  const { ok } = await apiRequest(`/api/articles/${id}`, { method: "DELETE" });
  if (!ok) {
    showToast("Verschieben fehlgeschlagen");
    return;
  }
  showToast("Artikel in den Papierkorb verschoben");
  showView("list");
  await runSearch();
}

async function restoreArticle(id) {
  const { ok } = await apiRequest(`/api/articles/${id}/restore`, { method: "POST" });
  if (!ok) {
    showToast("Wiederherstellen fehlgeschlagen");
    return;
  }
  showToast("Artikel wiederhergestellt");
  await openArticle(id);
}

async function permanentDeleteArticle(id) {
  if (!confirm("Artikel wirklich endgültig inkl. aller Revisionen löschen?")) return;
  const headers = {};
  if (state.meta.admin_protected) {
    const pw = prompt("Admin-Passwort zum endgültigen Löschen eingeben:");
    if (pw === null) return;
    headers["X-Admin-Password"] = pw;
  }
  const { ok, body } = await apiRequest(`/api/articles/${id}/permanent`, {
    method: "DELETE",
    headers,
  });
  if (!ok) {
    showToast(body?.error || "Endgültiges Löschen fehlgeschlagen");
    return;
  }
  showToast("Artikel endgültig gelöscht");
  showView("list");
  await runSearch();
}

// --- Editor & Konflikterkennung ---

function openEditor(article = null) {
  state.editingArticleId = article ? article.id : null;
  state.editingVersion = article ? article.version : 0;
  state.conflictServerArticle = null;

  els.editorHeading.textContent = article
    ? `Artikel bearbeiten (Version ${article.version})`
    : "Neuer Artikel";
  els.editorLockBanner.classList.add("hidden");

  els.edTitle.value = article ? article.title : "";
  els.edTemplate.value = article ? article.template_type : "problem_solution";
  els.edCategory.value = article && article.category_id ? String(article.category_id) : "";
  els.edStatus.value = article ? article.status : "active";
  els.edTags.value = article ? (article.tags || []).join(", ") : "";
  els.edErrorCodes.value = article ? article.error_codes || "" : "";
  els.edSymptoms.value = article ? article.symptoms || "" : "";
  els.edSolution.value = article ? article.solution_body || "" : "";
  els.edSummary.value = "";
  els.edSummaryWrap.classList.toggle("hidden", !article);

  syncTemplateFields();
  showView("editor");
  els.edTitle.focus();
  refreshPreview();

  if (article) {
    startLockHeartbeat(article.id);
  }
}

function syncTemplateFields() {
  const isHowTo = els.edTemplate.value === "howto";
  els.edSymptomsWrap.classList.toggle("hidden", isHowTo);
  els.edSolutionLabel.textContent = isHowTo
    ? "Anleitung / Inhalt (Markdown)"
    : "Schritt-für-Schritt-Lösung (Markdown)";
}

function buildEditorPayload(force = false) {
  const catVal = els.edCategory.value ? Number(els.edCategory.value) : null;
  const tags = els.edTags.value
    .split(",")
    .map((t) => t.trim())
    .filter(Boolean);

  return {
    template_type: els.edTemplate.value,
    title: els.edTitle.value.trim(),
    category_id: catVal,
    status: els.edStatus.value,
    error_codes: els.edErrorCodes.value.trim(),
    symptoms: els.edTemplate.value === "howto" ? "" : els.edSymptoms.value,
    solution_body: els.edSolution.value,
    tags,
    author_name: getAuthor(),
    editor_name: getAuthor(),
    change_summary: els.edSummary.value.trim(),
    expected_version: state.editingVersion,
    force,
  };
}

async function saveEditor(force = false) {
  const payload = buildEditorPayload(force);
  if (!payload.title) {
    showToast("Bitte einen Titel eingeben");
    els.edTitle.focus();
    return;
  }

  const isUpdate = state.editingArticleId !== null;
  const url = isUpdate ? `/api/articles/${state.editingArticleId}` : "/api/articles";
  const method = isUpdate ? "PUT" : "POST";

  const { ok, status, body } = await apiRequest(url, {
    method,
    body: JSON.stringify(payload),
  });

  if (status === 409 && body?.server_article) {
    openConflictDialog(body.server_article, payload);
    return;
  }
  if (!ok || !body?.article) {
    showToast(body?.error || "Speichern fehlgeschlagen");
    return;
  }

  showToast(isUpdate ? `Gespeichert (Version ${body.article.version})` : "Artikel erstellt");
  stopLockHeartbeat();
  await openArticle(body.article.id);
}

function openConflictDialog(serverArt, localPayload) {
  state.conflictServerArticle = serverArt;
  els.conflictIntro.textContent = `Auf dem Server existiert bereits Version ${serverArt.version} (zuletzt geändert am ${new Date(serverArt.updated_at).toLocaleString("de-DE")}). Deine Eingaben sind nicht verloren.`;

  const fields = [
    ["Titel", serverArt.title || "", localPayload.title || ""],
    ["Fehlercodes", serverArt.error_codes || "", localPayload.error_codes || ""],
    ["Symptome / Ursache", serverArt.symptoms || "", localPayload.symptoms || ""],
    ["Lösung / Inhalt", serverArt.solution_body || "", localPayload.solution_body || ""],
    ["Tags", (serverArt.tags || []).join(", "), (localPayload.tags || []).join(", ")],
  ];

  els.conflictDiff.innerHTML = "";
  for (const [label, srvVal, locVal] of fields) {
    if (srvVal === locVal) continue;
    const block = document.createElement("div");
    block.className = "diff-field";
    block.innerHTML = `
      <div class="diff-field-title">${escapeText(label)}</div>
      <div class="diff-cols">
        <div class="diff-col"><span class="diff-col-label">Server-Stand (Version ${serverArt.version})</span>${escapeText(srvVal || "–")}</div>
        <div class="diff-col"><span class="diff-col-label">Dein lokaler Entwurf</span>${escapeText(locVal || "–")}</div>
      </div>
    `;
    els.conflictDiff.appendChild(block);
  }
  if (els.conflictDiff.children.length === 0) {
    els.conflictDiff.textContent = "Es gibt nur Metadaten-Unterschiede (Status/Kategorie).";
  }
  els.conflictDialog.showModal();
}

function startLockHeartbeat(articleId) {
  stopLockHeartbeat();
  const beat = async () => {
    const { ok, body } = await apiRequest(`/api/articles/${articleId}/lock`, {
      method: "POST",
      body: JSON.stringify({ editor: getAuthor() }),
    });
    if (ok && body?.is_other && body.lock?.editor) {
      els.editorLockBanner.textContent = `Hinweis: ${body.lock.editor} bearbeitet diesen Artikel ebenfalls gerade. Beim gleichzeitigen Speichern greift die Konflikterkennung.`;
      els.editorLockBanner.classList.remove("hidden");
    } else {
      els.editorLockBanner.classList.add("hidden");
    }
  };
  beat();
  state.lockInterval = setInterval(beat, 15000);
}

function stopLockHeartbeat() {
  if (state.lockInterval) {
    clearInterval(state.lockInterval);
    state.lockInterval = null;
  }
  if (state.editingArticleId !== null) {
    const id = state.editingArticleId;
    const editor = encodeURIComponent(getAuthor());
    fetch(`/api/articles/${id}/lock?editor=${editor}`, { method: "DELETE", keepalive: true }).catch(() => {});
  }
}

async function refreshPreview() {
  const { ok, body } = await apiRequest("/api/preview", {
    method: "POST",
    body: JSON.stringify({
      symptoms: els.edTemplate.value === "howto" ? "" : els.edSymptoms.value,
      solution_body: els.edSolution.value,
    }),
  });
  if (!ok || !body) return;
  let html = "";
  if (els.edTemplate.value !== "howto" && body.symptoms_html) {
    html += `<div class="detail-section-label">Symptome / Ursache</div>${body.symptoms_html}`;
  }
  html += `<div class="detail-section-label">${els.edTemplate.value === "howto" ? "Anleitung" : "Lösung"}</div>${body.solution_html || "<p><em>Noch kein Inhalt …</em></p>"}`;
  els.edPreviewContent.innerHTML = html;
}

function schedulePreview() {
  clearTimeout(previewTimer);
  previewTimer = setTimeout(refreshPreview, 180);
}

function applyMarkdownAction(action) {
  const ta = state.lastFocusedTextarea || els.edSolution;
  const start = ta.selectionStart;
  const end = ta.selectionEnd;
  const sel = ta.value.slice(start, end);
  let insert = sel;

  switch (action) {
    case "h3":
      insert = `\n### ${sel || "Überschrift"}\n`;
      break;
    case "bold":
      insert = `**${sel || "Text"}**`;
      break;
    case "code":
      insert = `\`${sel || "code"}\``;
      break;
    case "codeblock":
      insert = `\n\`\`\`powershell\n${sel || "# Befehl hier"}\n\`\`\`\n`;
      break;
    case "list":
      insert = `\n- ${sel || "Punkt 1"}\n- Punkt 2\n`;
      break;
    case "checklist":
      insert = `\n- [ ] ${sel || "Prüfschritt 1"}\n- [ ] Prüfschritt 2\n`;
      break;
  }

  ta.setRangeText(insert, start, end, "end");
  ta.focus();
  schedulePreview();
}

function attachCodeCopyButtons(container) {
  for (const pre of container.querySelectorAll("pre")) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "code-copy-btn";
    btn.textContent = "Kopieren";
    btn.addEventListener("click", async () => {
      const code = pre.querySelector("code");
      const text = code ? code.innerText : pre.innerText;
      await navigator.clipboard.writeText(text);
      btn.textContent = "Kopiert";
      setTimeout(() => (btn.textContent = "Kopieren"), 1500);
    });
    pre.appendChild(btn);
  }
}

function formatStatus(s) {
  switch (s) {
    case "active": return "Aktiv";
    case "draft": return "Entwurf";
    case "outdated": return "Veraltet";
    default: return s;
  }
}

function escapeText(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function bindEvents() {
  els.siteTitle.addEventListener("click", (e) => {
    e.preventDefault();
    showView("list");
    runSearch();
  });

  els.btnNewArticle.addEventListener("click", () => openEditor(null));
  els.btnAdminView.addEventListener("click", async () => {
    showView("admin");
    await adminUI.refreshAdminView();
  });
  els.btnAdminClose.addEventListener("click", () => {
    showView("list");
    runSearch();
  });
  els.btnEditorCancel.addEventListener("click", () => {
    const backId = state.editingArticleId;
    stopLockHeartbeat();
    state.editingArticleId = null;
    if (backId) {
      openArticle(backId);
    } else {
      showView("list");
      runSearch();
    }
  });
  els.btnEditorSave.addEventListener("click", () => saveEditor(false));

  els.edTemplate.addEventListener("change", () => {
    syncTemplateFields();
    schedulePreview();
  });
  els.edSymptoms.addEventListener("input", schedulePreview);
  els.edSolution.addEventListener("input", schedulePreview);
  els.edSymptoms.addEventListener("focus", () => (state.lastFocusedTextarea = els.edSymptoms));
  els.edSolution.addEventListener("focus", () => (state.lastFocusedTextarea = els.edSolution));

  for (const btn of document.querySelectorAll(".seg-btn")) {
    btn.addEventListener("click", () => {
      document.querySelectorAll(".seg-btn").forEach((b) => b.classList.remove("active"));
      btn.classList.add("active");
      els.editorPanes.className = `editor-panes view-${btn.dataset.view}`;
      if (btn.dataset.view !== "write") refreshPreview();
    });
  }

  for (const btn of document.querySelectorAll(".tool-btn[data-md]")) {
    btn.addEventListener("click", () => applyMarkdownAction(btn.dataset.md));
  }

  bindMediaInputsToTextarea(els.edSymptoms);
  bindMediaInputsToTextarea(els.edSolution);

  els.btnUploadMedia.addEventListener("click", () => els.mediaFileInput.click());
  els.mediaFileInput.addEventListener("change", async () => {
    if (els.mediaFileInput.files?.length) {
      await handleFilesUpload(els.mediaFileInput.files, state.lastFocusedTextarea || els.edSolution);
      els.mediaFileInput.value = "";
    }
  });

  els.lightboxClose.addEventListener("click", () => els.lightboxDialog.close());
  els.lightboxDialog.addEventListener("click", (e) => {
    if (e.target === els.lightboxDialog) els.lightboxDialog.close();
  });

  // Konfliktdialog
  els.btnConflictCancel.addEventListener("click", () => els.conflictDialog.close());
  els.btnConflictTakeServer.addEventListener("click", () => {
    els.conflictDialog.close();
    if (state.conflictServerArticle) {
      openEditor(state.conflictServerArticle);
      showToast("Server-Stand in Editor übernommen");
    }
  });
  els.btnConflictForce.addEventListener("click", () => {
    els.conflictDialog.close();
    saveEditor(true);
  });

  // Suche & Filter
  const triggerSearch = () => {
    showView("list");
    clearTimeout(searchTimer);
    searchTimer = setTimeout(runSearch, 110);
  };
  els.searchInput.addEventListener("input", triggerSearch);
  els.filterCategory.addEventListener("change", runSearch);
  els.filterStatus.addEventListener("change", runSearch);
  els.filterTemplate.addEventListener("change", runSearch);
  els.filterNeedsReview.addEventListener("change", runSearch);
  els.filterDeleted.addEventListener("change", runSearch);

  // Tastenkürzel
  document.addEventListener("keydown", (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s" && !els.articleEditor.classList.contains("hidden")) {
      e.preventDefault();
      saveEditor(false);
      return;
    }
    if ((e.key === "/" || (e.ctrlKey && e.key.toLowerCase() === "k")) && document.activeElement !== els.searchInput) {
      if (document.activeElement.tagName !== "INPUT" && document.activeElement.tagName !== "TEXTAREA") {
        e.preventDefault();
        showView("list");
        els.searchInput.focus();
      }
    }
  });

  window.addEventListener("beforeunload", () => stopLockHeartbeat());
}

const adminUI = createAdminAndHistoryController({
  apiRequest,
  getAuthor,
  getMeta: () => state.meta,
  reloadMeta: loadMeta,
  onRestoredArticle: (id) => openArticle(id),
  showToast,
});

async function boot() {
  initTheme();
  initAuthor();
  bindEvents();
  await loadMeta();
  await runSearch();
}

boot().catch((err) => {
  console.error("Initialisierung fehlgeschlagen:", err);
  els.resultsList.textContent = "Fehler beim Laden der Wissensdatenbank.";
});
