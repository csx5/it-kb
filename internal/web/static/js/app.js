const THEME_KEY = "itkb_theme";
const AUTHOR_KEY = "itkb_author";

const els = {
  siteTitle: document.getElementById("site-title"),
  appVersion: document.getElementById("app-version"),
  authorInput: document.getElementById("author-input"),
  themeToggle: document.getElementById("theme-toggle"),
  searchInput: document.getElementById("search-input"),
  filterCategory: document.getElementById("filter-category"),
  filterStatus: document.getElementById("filter-status"),
  filterTemplate: document.getElementById("filter-template"),
  filterNeedsReview: document.getElementById("filter-needs-review"),
  resultMeta: document.getElementById("result-meta"),
  resultsList: document.getElementById("results-list"),
  articleDetail: document.getElementById("article-detail"),
};

let searchTimer = null;

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

async function fetchJSON(url) {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }
  return res.json();
}

async function loadMeta() {
  const meta = await fetchJSON("/api/meta");
  els.siteTitle.textContent = meta.site_title || "IT-Wissensdatenbank";
  document.title = els.siteTitle.textContent;
  els.appVersion.textContent = meta.version || "dev";

  for (const cat of meta.categories || []) {
    const opt = document.createElement("option");
    opt.value = String(cat.id);
    opt.textContent = cat.name;
    els.filterCategory.appendChild(opt);
  }
}

async function runSearch() {
  const params = new URLSearchParams();
  if (els.searchInput.value.trim()) params.set("q", els.searchInput.value.trim());
  if (els.filterCategory.value) params.set("category_id", els.filterCategory.value);
  if (els.filterStatus.value) params.set("status", els.filterStatus.value);
  if (els.filterTemplate.value) params.set("template", els.filterTemplate.value);
  if (els.filterNeedsReview.checked) params.set("needs_review", "true");

  const data = await fetchJSON(`/api/search?${params.toString()}`);
  els.resultMeta.textContent = `${data.count} Treffer`;
  renderResults(data.hits || []);
}

function renderResults(hits) {
  els.resultsList.innerHTML = "";
  if (hits.length === 0) {
    const empty = document.createElement("p");
    empty.className = "hit-snippet";
    empty.textContent = "Keine passenden Einträge gefunden.";
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

    const meta = document.createElement("span");
    meta.className = "badge";
    meta.textContent = a.category_name || "Ohne Kategorie";
    header.appendChild(meta);
    card.appendChild(header);

    const badges = document.createElement("div");
    badges.className = "hit-badges";
    if (a.error_codes) {
      const b = document.createElement("span");
      b.className = "badge badge-code";
      b.textContent = a.error_codes;
      badges.appendChild(b);
    }
    if (hit.needs_review) {
      const w = document.createElement("span");
      w.className = "badge badge-warn";
      w.textContent = "Prüfung fällig";
      badges.appendChild(w);
    }
    for (const tag of a.tags || []) {
      const t = document.createElement("span");
      t.className = "badge";
      t.textContent = `#${tag}`;
      badges.appendChild(t);
    }
    card.appendChild(badges);

    const snippet = document.createElement("p");
    snippet.className = "hit-snippet";
    // Snippet kommt bereits HTML-escaped (mit <mark>) vom Server
    snippet.innerHTML = hit.snippet_html || "";
    card.appendChild(snippet);

    els.resultsList.appendChild(card);
  }
}

async function openArticle(id) {
  const data = await fetchJSON(`/api/articles/${id}`);
  const a = data.article;

  els.articleDetail.classList.remove("hidden");
  els.articleDetail.innerHTML = "";

  const backBtn = document.createElement("button");
  backBtn.type = "button";
  backBtn.className = "btn";
  backBtn.textContent = "Zurück zur Übersicht";
  backBtn.addEventListener("click", () => {
    els.articleDetail.classList.add("hidden");
    els.resultsList.classList.remove("hidden");
  });

  const heading = document.createElement("h2");
  heading.textContent = a.title;

  const metaLine = document.createElement("div");
  metaLine.className = "hit-badges";
  metaLine.innerHTML = `
    <span class="badge">${escapeText(a.category_name || "Ohne Kategorie")}</span>
    <span class="badge">Status: ${escapeText(a.status)}</span>
    <span class="badge">Version ${a.version}</span>
    <span class="badge">Autor: ${escapeText(a.author_name || "Unbekannt")}</span>
  `;

  els.articleDetail.append(backBtn, heading, metaLine);

  if (a.error_codes) {
    const lbl = document.createElement("div");
    lbl.className = "detail-section-label";
    lbl.textContent = "Fehlercode / Fehlermeldung";
    const box = document.createElement("pre");
    box.textContent = a.error_codes;
    els.articleDetail.append(lbl, box);
  }

  if (data.symptoms_html) {
    const lbl = document.createElement("div");
    lbl.className = "detail-section-label";
    lbl.textContent = "Symptome / Ursache";
    const div = document.createElement("div");
    div.innerHTML = data.symptoms_html;
    els.articleDetail.append(lbl, div);
  }

  const solLbl = document.createElement("div");
  solLbl.className = "detail-section-label";
  solLbl.textContent = a.template_type === "howto" ? "Anleitung" : "Lösung";
  const solDiv = document.createElement("div");
  solDiv.innerHTML = data.solution_html || "";
  els.articleDetail.append(solLbl, solDiv);

  attachCodeCopyButtons(els.articleDetail);
  els.resultsList.classList.add("hidden");
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

function escapeText(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  }[c]));
}

function bindEvents() {
  const triggerSearch = () => {
    els.articleDetail.classList.add("hidden");
    els.resultsList.classList.remove("hidden");
    clearTimeout(searchTimer);
    searchTimer = setTimeout(runSearch, 110);
  };

  els.searchInput.addEventListener("input", triggerSearch);
  els.filterCategory.addEventListener("change", runSearch);
  els.filterStatus.addEventListener("change", runSearch);
  els.filterTemplate.addEventListener("change", runSearch);
  els.filterNeedsReview.addEventListener("change", runSearch);

  document.addEventListener("keydown", (e) => {
    if ((e.key === "/" || (e.ctrlKey && e.key.toLowerCase() === "k")) && document.activeElement !== els.searchInput) {
      if (document.activeElement.tagName !== "INPUT" && document.activeElement.tagName !== "TEXTAREA") {
        e.preventDefault();
        els.searchInput.focus();
      }
    }
  });
}

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
