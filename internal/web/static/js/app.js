import { createImageEditorController } from "/js/editor-canvas.js";
import { createAdminAndHistoryController } from "/js/admin-ui.js";
import { scanForSensitiveData } from "/js/pii-scanner.js";

const THEME_KEY = "itkb_theme";
const AUTHOR_KEY = "itkb_author";
const HELPFUL_KEY = "itkb_helpful_ids";

const imageEditor = createImageEditorController();

const els = {
  authScreen: document.getElementById("auth-screen"),
  authSiteTitle: document.getElementById("auth-site-title"),
  authSubtitle: document.getElementById("auth-subtitle"),
  authLoginForm: document.getElementById("auth-login-form"),
  loginUsername: document.getElementById("login-username"),
  loginPassword: document.getElementById("login-password"),
  loginTotpWrap: document.getElementById("login-totp-wrap"),
  loginTotpCode: document.getElementById("login-totp-code"),
  loginError: document.getElementById("login-error"),
  authSetupForm: document.getElementById("auth-setup-form"),
  setupUsername: document.getElementById("setup-username"),
  setupDisplayName: document.getElementById("setup-display-name"),
  setupPassword: document.getElementById("setup-password"),
  setupPasswordConfirm: document.getElementById("setup-password-confirm"),
  setupError: document.getElementById("setup-error"),
  appShell: document.getElementById("app-shell"),
  siteTitle: document.getElementById("site-title"),
  appVersion: document.getElementById("app-version"),
  btnNewArticle: document.getElementById("btn-new-article"),
  btnAdminView: document.getElementById("btn-admin-view"),
  adminPanel: document.getElementById("admin-panel"),
  btnAdminClose: document.getElementById("btn-admin-close"),
  authorBoxWrap: document.getElementById("author-box-wrap"),
  authorInput: document.getElementById("author-input"),
  userMenu: document.getElementById("user-menu"),
  userBadge: document.getElementById("user-badge"),
  btnChangePw: document.getElementById("btn-change-pw"),
  btnLogout: document.getElementById("btn-logout"),
  passwordDialog: document.getElementById("password-dialog"),
  changePwForm: document.getElementById("change-pw-form"),
  pwCurrent: document.getElementById("pw-current"),
  pwNew: document.getElementById("pw-new"),
  pwConfirm: document.getElementById("pw-confirm"),
  btnPwCancel: document.getElementById("btn-pw-cancel"),
  totpStatusText: document.getElementById("totp-status-text"),
  totpSetupArea: document.getElementById("totp-setup-area"),
  totpSecretCode: document.getElementById("totp-secret-code"),
  btnTotpCopy: document.getElementById("btn-totp-copy"),
  totpConfirmCode: document.getElementById("totp-confirm-code"),
  btnTotpConfirm: document.getElementById("btn-totp-confirm"),
  btnTotpStart: document.getElementById("btn-totp-start"),
  btnTotpDisable: document.getElementById("btn-totp-disable"),
  piiDialog: document.getElementById("pii-dialog"),
  piiFindingsList: document.getElementById("pii-findings-list"),
  btnPiiCancel: document.getElementById("btn-pii-cancel"),
  btnPiiProceed: document.getElementById("btn-pii-proceed"),
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
  meta: { categories: [], admin_protected: false, auth_required: true },
  user: null,
  authRequired: true,
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

function canEdit() {
  if (!state.authRequired) return true;
  return state.user && (state.user.role === "editor" || state.user.role === "admin");
}

function isAdmin() {
  if (!state.authRequired) return true;
  return state.user && state.user.role === "admin";
}

function formatRole(role) {
  switch (role) {
    case "admin": return "Administrator";
    case "editor": return "Bearbeiter";
    case "reader": return "Leser";
    default: return role || "";
  }
}

function getAuthor() {
  if (state.user) {
    return state.user.display_name || state.user.username;
  }
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
  const { headers, ...rest } = options;
  const res = await fetch(url, {
    ...rest,
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "it-kb",
      ...(headers || {}),
    },
  });
  let body = null;
  try {
    body = await res.json();
  } catch {
    body = null;
  }
  if (res.status === 401 && state.authRequired && !url.startsWith("/api/auth/")) {
    stopLockHeartbeat();
    state.user = null;
    showAuthScreen(false);
  }
  return { ok: res.ok, status: res.status, body };
}

function applyRoleUI() {
  els.authorBoxWrap.classList.toggle("hidden", state.authRequired);
  els.userMenu.classList.toggle("hidden", !state.authRequired || !state.user);
  if (state.user) {
    els.userBadge.textContent = `${state.user.display_name || state.user.username} (${formatRole(state.user.role)})`;
  }
  els.btnNewArticle.classList.toggle("hidden", !canEdit());
  els.btnAdminView.classList.toggle("hidden", !canEdit());
  els.btnAdminView.textContent = isAdmin() ? "Admin-Konsole" : "Verwaltung";
}

function showAuthScreen(needsSetup, siteTitle = "") {
  els.appShell.classList.add("hidden");
  els.authScreen.classList.remove("hidden");
  if (siteTitle) {
    els.authSiteTitle.textContent = siteTitle;
    document.title = siteTitle;
  }
  els.loginError.classList.add("hidden");
  els.setupError.classList.add("hidden");

  if (needsSetup) {
    els.authSubtitle.textContent = "Ersteinrichtung: Administrator anlegen";
    els.authSetupForm.classList.remove("hidden");
    els.authLoginForm.classList.add("hidden");
    els.setupUsername.focus();
  } else {
    els.authSubtitle.textContent = "Anmeldung";
    els.authLoginForm.classList.remove("hidden");
    els.authSetupForm.classList.add("hidden");
    els.loginUsername.focus();
  }
}

function showAppShell() {
  els.authScreen.classList.add("hidden");
  els.appShell.classList.remove("hidden");
  applyRoleUI();
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
    if (els.filterDeleted.checked) {
      empty.textContent = "Der Papierkorb ist leer.";
    } else if (!els.searchInput.value.trim() && !els.filterCategory.value && !els.filterStatus.value && !els.filterTemplate.value && !els.filterNeedsReview.checked) {
      empty.textContent = "Noch keine Einträge vorhanden.";
    } else {
      empty.textContent = "Keine passenden Einträge gefunden.";
    }
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
    if (a.is_pinned) {
      badges.appendChild(makeBadge("Angepinnt", "badge-pin"));
    }
    if (a.helpful_count > 0) {
      badges.appendChild(makeBadge(`Hilfreich: ${a.helpful_count}`, "badge-ok"));
    }
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
    const helpfulBtn = document.createElement("button");
    helpfulBtn.type = "button";
    helpfulBtn.className = "btn";
    const alreadyHelpful = getHelpfulSet().has(a.id);
    helpfulBtn.textContent = alreadyHelpful
      ? `Hilfreich (${a.helpful_count})`
      : `Hat geholfen (${a.helpful_count})`;
    helpfulBtn.disabled = alreadyHelpful;
    helpfulBtn.addEventListener("click", () => markHelpful(a.id));
    actions.appendChild(helpfulBtn);

    const printBtn = document.createElement("button");
    printBtn.type = "button";
    printBtn.className = "btn";
    printBtn.textContent = "Drucken / PDF";
    printBtn.addEventListener("click", () => window.print());
    actions.appendChild(printBtn);

    const historyBtn = document.createElement("button");
    historyBtn.type = "button";
    historyBtn.className = "btn";
    historyBtn.textContent = `Historie (v${a.version})`;
    historyBtn.addEventListener("click", () => adminUI.openHistory(a.id));
    actions.appendChild(historyBtn);

    if (canEdit()) {
      const pinBtn = document.createElement("button");
      pinBtn.type = "button";
      pinBtn.className = "btn";
      pinBtn.textContent = a.is_pinned ? "Lösen" : "Anpinnen";
      pinBtn.addEventListener("click", () => togglePin(a.id, !a.is_pinned));

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

      actions.append(pinBtn, verifyBtn, editBtn, delBtn);
    }
  } else {
    if (canEdit()) {
      const restoreBtn = document.createElement("button");
      restoreBtn.type = "button";
      restoreBtn.className = "btn btn-primary";
      restoreBtn.textContent = "Wiederherstellen";
      restoreBtn.addEventListener("click", () => restoreArticle(a.id));
      actions.appendChild(restoreBtn);
    }

    if (isAdmin()) {
      const permBtn = document.createElement("button");
      permBtn.type = "button";
      permBtn.className = "btn btn-danger";
      permBtn.textContent = "Endgültig löschen";
      permBtn.addEventListener("click", () => permanentDeleteArticle(a.id));
      actions.appendChild(permBtn);
    }
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
    const code = document.createElement("code");
    code.textContent = a.error_codes;
    box.appendChild(code);
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

  // Ähnliche Artikel (rein algorithmisch)
  if (Array.isArray(body.related_articles) && body.related_articles.length > 0) {
    const relSec = document.createElement("section");
    relSec.className = "related-section";
    const relTitle = document.createElement("div");
    relTitle.className = "detail-section-label";
    relTitle.textContent = "Ähnliche Artikel";
    const grid = document.createElement("div");
    grid.className = "related-grid";
    for (const rel of body.related_articles) {
      const card = document.createElement("div");
      card.className = "related-card";
      card.addEventListener("click", () => openArticle(rel.id));
      card.innerHTML = `
        <div class="related-card-title">${escapeText(rel.title)}</div>
        <div class="hit-badges">
          <span class="badge">${escapeText(rel.category_name || "Ohne Kategorie")}</span>
          ${rel.error_codes ? `<span class="badge badge-code">${escapeText(rel.error_codes)}</span>` : ""}
        </div>
      `;
      grid.appendChild(card);
    }
    relSec.append(relTitle, grid);
    els.articleDetail.appendChild(relSec);
  }

  // Praxis-Hinweise & Kommentare
  if (!a.deleted_at) {
    renderCommentsSection(a.id, body.comments || []);
  }
}

function getHelpfulSet() {
  try {
    return new Set(JSON.parse(localStorage.getItem(HELPFUL_KEY) || "[]"));
  } catch {
    return new Set();
  }
}

async function markHelpful(id) {
  const { ok } = await apiRequest(`/api/articles/${id}/helpful`, { method: "POST", body: "{}" });
  if (!ok) {
    showToast("Aktion fehlgeschlagen");
    return;
  }
  const set = getHelpfulSet();
  set.add(id);
  localStorage.setItem(HELPFUL_KEY, JSON.stringify([...set]));
  showToast("Danke für die Rückmeldung");
  await openArticle(id);
}

async function togglePin(id, pinned) {
  const { ok } = await apiRequest(`/api/articles/${id}/pin`, {
    method: "POST",
    body: JSON.stringify({ pinned }),
  });
  if (!ok) {
    showToast("Pin-Status konnte nicht geändert werden");
    return;
  }
  showToast(pinned ? "Artikel oben angepinnt" : "Anpinnen aufgehoben");
  await openArticle(id);
}

function renderCommentsSection(articleId, comments) {
  const sec = document.createElement("section");
  sec.className = "comments-section";

  const lbl = document.createElement("div");
  lbl.className = "detail-section-label";
  lbl.textContent = `Praxis-Hinweise & Kommentare (${comments.length})`;
  sec.appendChild(lbl);

  const list = document.createElement("div");
  list.className = "comments-list";
  if (comments.length === 0) {
    const empty = document.createElement("p");
    empty.className = "hit-snippet";
    empty.textContent = "Noch keine Praxis-Hinweise vorhanden.";
    list.appendChild(empty);
  } else {
    for (const c of comments) {
      const card = document.createElement("div");
      card.className = "comment-card";
      const meta = document.createElement("div");
      meta.className = "comment-meta";
      const left = document.createElement("span");
      left.textContent = `${c.author_name || "Unbekannt"} · ${new Date(c.created_at).toLocaleString("de-DE")}`;
      meta.appendChild(left);

      const canDelete = !state.authRequired || isAdmin() || (state.user && c.author_user_id === state.user.id);
      if (canDelete) {
        const del = document.createElement("button");
        del.type = "button";
        del.className = "btn btn-sm";
        del.textContent = "Entfernen";
        del.addEventListener("click", async () => {
          if (!confirm("Hinweis wirklich entfernen?")) return;
          const res = await apiRequest(`/api/articles/${articleId}/comments/${c.id}`, { method: "DELETE" });
          if (res.ok) {
            showToast("Hinweis entfernt");
            openArticle(articleId);
          }
        });
        meta.appendChild(del);
      }

      const bodyEl = document.createElement("div");
      bodyEl.className = "comment-body";
      bodyEl.textContent = c.body;
      card.append(meta, bodyEl);
      list.appendChild(card);
    }
  }
  sec.appendChild(list);

  const form = document.createElement("form");
  form.className = "comments-form";
  const ta = document.createElement("textarea");
  ta.rows = 3;
  ta.placeholder = "Hinweis schreiben …";
  const submit = document.createElement("button");
  submit.type = "submit";
  submit.className = "btn";
  submit.style.alignSelf = "flex-start";
  submit.textContent = "Hinweis hinzufügen";
  form.append(ta, submit);

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const text = ta.value.trim();
    if (!text) return;
    const res = await apiRequest(`/api/articles/${articleId}/comments`, {
      method: "POST",
      body: JSON.stringify({ body: text, author_name: getAuthor() }),
    });
    if (!res.ok) {
      showToast(res.body?.error || "Hinweis konnte nicht gespeichert werden");
      return;
    }
    showToast("Hinweis hinzugefügt");
    await openArticle(articleId);
  });

  sec.appendChild(form);
  els.articleDetail.appendChild(sec);
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

  const res = await fetch("/api/media", {
    method: "POST",
    headers: { "X-Requested-With": "it-kb" },
    body: fd,
  });
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
  let edited = null;
  try {
    edited = await imageEditor.open(fileOrBlob, suggestedName);
  } catch {
    showToast("Bild konnte nicht geladen werden");
    return;
  }
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
  state.lastFocusedTextarea = els.edSolution;
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
  els.edSolutionLabel.textContent = isHowTo ? "Anleitung" : "Lösung";
  if (isHowTo) {
    state.lastFocusedTextarea = els.edSolution;
  }
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

async function saveEditor(force = false, skipPIICheck = false) {
  const payload = buildEditorPayload(force);
  if (!payload.title) {
    showToast("Bitte einen Titel eingeben");
    els.edTitle.focus();
    return;
  }

  // Lokaler Sensible-Daten-Warner (E-Mails, Klartext-Passwörter, Keys, IBANs, interne IPs)
  if (!skipPIICheck) {
    const findings = scanForSensitiveData([
      { label: "Titel", value: payload.title },
      { label: "Fehlercodes", value: payload.error_codes },
      { label: "Symptome / Ursache", value: payload.symptoms },
      { label: "Lösung / Inhalt", value: payload.solution_body },
    ]);
    if (findings.length > 0) {
      const proceed = await confirmPIIDialog(findings);
      if (!proceed) return;
    }
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
  els.conflictIntro.textContent = `Server-Stand: Version ${serverArt.version} (${new Date(serverArt.updated_at).toLocaleString("de-DE")}).`;

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
      els.editorLockBanner.textContent = `Wird gerade von ${body.lock.editor} bearbeitet.`;
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
    state.editingArticleId = null;
    const editor = encodeURIComponent(getAuthor());
    return fetch(`/api/articles/${id}/lock?editor=${editor}`, {
      method: "DELETE",
      headers: { "X-Requested-With": "it-kb" },
      keepalive: true,
    }).catch(() => {});
  }
  return Promise.resolve();
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

async function copyTextToClipboard(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    await navigator.clipboard.writeText(text);
    return;
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  const ok = document.execCommand("copy");
  ta.remove();
  if (!ok) {
    throw new Error("Kopieren nicht möglich");
  }
}

function attachCodeCopyButtons(container) {
  for (const pre of container.querySelectorAll("pre")) {
    const code = pre.querySelector("code");
    const rawText = code ? code.innerText : pre.innerText;
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "code-copy-btn";
    btn.textContent = "Kopieren";
    btn.addEventListener("click", async () => {
      try {
        await copyTextToClipboard(rawText);
        btn.textContent = "Kopiert";
        setTimeout(() => (btn.textContent = "Kopieren"), 1500);
      } catch {
        showToast("Kopieren fehlgeschlagen");
      }
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
  els.btnEditorCancel.addEventListener("click", async () => {
    const backId = state.editingArticleId;
    await stopLockHeartbeat();
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

  for (const btn of document.querySelectorAll(".seg-btn[data-view]")) {
    btn.addEventListener("click", () => {
      document.querySelectorAll(".seg-btn[data-view]").forEach((b) => b.classList.remove("active"));
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
    saveEditor(true, true);
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
      if (els.appShell.classList.contains("hidden")) return;
      if (document.querySelector("dialog[open]")) return;
      if (!els.articleEditor.classList.contains("hidden")) return;
      const tag = document.activeElement?.tagName;
      if (tag !== "INPUT" && tag !== "TEXTAREA" && tag !== "SELECT" && tag !== "OPTION" && tag !== "BUTTON") {
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
  getUser: () => state.user,
  canEdit,
  isAdmin,
  reloadMeta: loadMeta,
  onRestoredArticle: (id) => openArticle(id),
  showToast,
});

function confirmPIIDialog(findings) {
  els.piiFindingsList.innerHTML = "";
  for (const f of findings) {
    const row = document.createElement("div");
    row.className = "pii-item";
    row.innerHTML = `
      <span><strong>${escapeText(f.category)}</strong> im Feld <em>${escapeText(f.field)}</em></span>
      <code>${escapeText(f.sample)}</code>
    `;
    els.piiFindingsList.appendChild(row);
  }
  els.piiDialog.showModal();
  return new Promise((resolve) => {
    const cleanup = (val) => {
      els.btnPiiCancel.removeEventListener("click", onCancel);
      els.btnPiiProceed.removeEventListener("click", onProceed);
      els.piiDialog.removeEventListener("cancel", onDialogCancel);
      if (els.piiDialog.open) els.piiDialog.close();
      resolve(val);
    };
    const onCancel = () => cleanup(false);
    const onProceed = () => cleanup(true);
    const onDialogCancel = (e) => {
      e.preventDefault();
      cleanup(false);
    };
    els.btnPiiCancel.addEventListener("click", onCancel);
    els.btnPiiProceed.addEventListener("click", onProceed);
    els.piiDialog.addEventListener("cancel", onDialogCancel);
  });
}

function updateTOTPDialogState() {
  const enabled = Boolean(state.user && state.user.totp_enabled);
  els.totpStatusText.textContent = enabled ? "Status: Aktiv" : "Status: Nicht aktiviert";
  els.totpSetupArea.classList.add("hidden");
  els.btnTotpStart.classList.toggle("hidden", enabled);
  els.btnTotpDisable.classList.toggle("hidden", !enabled);
}

function bindAuthEvents() {
  els.loginUsername.addEventListener("input", () => {
    els.loginTotpWrap.classList.add("hidden");
    els.loginTotpCode.value = "";
  });

  els.authLoginForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    els.loginError.classList.add("hidden");
    const { ok, body } = await apiRequest("/api/auth/login", {
      method: "POST",
      body: JSON.stringify({
        username: els.loginUsername.value.trim(),
        password: els.loginPassword.value,
        totp_code: els.loginTotpCode.value.trim(),
      }),
    });
    if (ok && body?.totp_required) {
      els.loginTotpWrap.classList.remove("hidden");
      els.loginTotpCode.focus();
      return;
    }
    if (!ok || !body?.user) {
      els.loginError.textContent = body?.error || "Anmeldung fehlgeschlagen";
      els.loginError.classList.remove("hidden");
      if (els.loginTotpWrap.classList.contains("hidden")) {
        els.loginPassword.value = "";
        els.loginPassword.focus();
      } else {
        els.loginTotpCode.value = "";
        els.loginTotpCode.focus();
      }
      return;
    }
    state.user = body.user;
    els.loginPassword.value = "";
    els.loginTotpCode.value = "";
    els.loginTotpWrap.classList.add("hidden");
    showAppShell();
    await loadMeta();
    await runSearch();
  });

  els.authSetupForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    els.setupError.classList.add("hidden");
    if (els.setupPassword.value !== els.setupPasswordConfirm.value) {
      els.setupError.textContent = "Die beiden Passwörter stimmen nicht überein.";
      els.setupError.classList.remove("hidden");
      return;
    }
    const { ok, body } = await apiRequest("/api/auth/setup", {
      method: "POST",
      body: JSON.stringify({
        username: els.setupUsername.value.trim(),
        display_name: els.setupDisplayName.value.trim(),
        password: els.setupPassword.value,
      }),
    });
    if (!ok || !body?.user) {
      els.setupError.textContent = body?.error || "Einrichtung fehlgeschlagen";
      els.setupError.classList.remove("hidden");
      return;
    }
    state.user = body.user;
    els.setupPassword.value = "";
    els.setupPasswordConfirm.value = "";
    showAppShell();
    showToast("Administrator erfolgreich angelegt");
    await loadMeta();
    await runSearch();
  });

  els.btnLogout.addEventListener("click", async () => {
    await stopLockHeartbeat();
    await apiRequest("/api/auth/logout", { method: "POST" });
    state.user = null;
    els.resultsList.innerHTML = "";
    els.articleDetail.innerHTML = "";
    els.searchInput.value = "";
    els.edTitle.value = "";
    els.edErrorCodes.value = "";
    els.edSymptoms.value = "";
    els.edSolution.value = "";
    const bp = document.getElementById("backup-passphrase");
    if (bp) bp.value = "";
    const apw = document.getElementById("admin-password-input");
    if (apw) apw.value = "";
    const ul = document.getElementById("admin-users-list");
    if (ul) ul.innerHTML = "";
    const ab = document.getElementById("admin-audit-body");
    if (ab) ab.innerHTML = "";
    showView("list");
    showAuthScreen(false, state.meta.site_title);
  });

  els.btnChangePw.addEventListener("click", () => {
    els.pwCurrent.value = "";
    els.pwNew.value = "";
    els.pwConfirm.value = "";
    els.totpConfirmCode.value = "";
    updateTOTPDialogState();
    els.passwordDialog.showModal();
    els.pwCurrent.focus();
  });
  els.btnPwCancel.addEventListener("click", () => els.passwordDialog.close());

  els.btnTotpStart.addEventListener("click", async () => {
    const { ok, body } = await apiRequest("/api/auth/totp/setup", { method: "POST", body: "{}" });
    if (!ok || !body?.secret) {
      showToast(body?.error || "2FA-Start fehlgeschlagen");
      return;
    }
    els.totpSecretCode.textContent = body.secret;
    els.totpSetupArea.classList.remove("hidden");
    els.btnTotpStart.classList.add("hidden");
    els.totpConfirmCode.focus();
  });

  els.btnTotpCopy.addEventListener("click", async () => {
    try {
      await copyTextToClipboard(els.totpSecretCode.textContent || "");
      showToast("2FA-Schlüssel kopiert");
    } catch {
      showToast("Kopieren fehlgeschlagen");
    }
  });

  els.btnTotpConfirm.addEventListener("click", async () => {
    const code = els.totpConfirmCode.value.trim();
    const { ok, body } = await apiRequest("/api/auth/totp/enable", {
      method: "POST",
      body: JSON.stringify({ code }),
    });
    if (!ok) {
      showToast(body?.error || "2FA-Code ungültig");
      return;
    }
    if (state.user) state.user.totp_enabled = true;
    updateTOTPDialogState();
    showToast("2FA (TOTP) erfolgreich aktiviert");
  });

  els.btnTotpDisable.addEventListener("click", async () => {
    const pw = prompt("Aktuelles Passwort zum Deaktivieren von 2FA eingeben:");
    if (!pw) return;
    const { ok, body } = await apiRequest("/api/auth/totp/disable", {
      method: "POST",
      body: JSON.stringify({ current_password: pw }),
    });
    if (!ok) {
      showToast(body?.error || "2FA konnte nicht deaktiviert werden");
      return;
    }
    if (state.user) state.user.totp_enabled = false;
    updateTOTPDialogState();
    showToast("2FA deaktiviert");
  });
  els.changePwForm.addEventListener("submit", async (e) => {
    e.preventDefault();
    if (els.pwNew.value !== els.pwConfirm.value) {
      showToast("Neue Passwörter stimmen nicht überein");
      return;
    }
    const { ok, body } = await apiRequest("/api/auth/password", {
      method: "POST",
      body: JSON.stringify({
        current_password: els.pwCurrent.value,
        new_password: els.pwNew.value,
      }),
    });
    if (!ok) {
      showToast(body?.error || "Passwortänderung fehlgeschlagen");
      return;
    }
    els.passwordDialog.close();
    showToast("Passwort erfolgreich geändert");
  });
}

async function boot() {
  initTheme();
  initAuthor();
  bindEvents();
  bindAuthEvents();

  const { ok, body } = await apiRequest("/api/auth/status");
  if (!ok || !body) {
    throw new Error("Serverstatus konnte nicht geladen werden");
  }
  state.authRequired = Boolean(body.auth_required);
  state.user = body.user || null;

  if (state.authRequired && !body.authenticated) {
    showAuthScreen(Boolean(body.needs_setup), body.site_title);
    return;
  }

  showAppShell();
  await loadMeta();
  await runSearch();
}

boot().catch((err) => {
  console.error("Initialisierung fehlgeschlagen:", err);
  els.resultsList.textContent = "Fehler beim Laden der Wissensdatenbank.";
});
