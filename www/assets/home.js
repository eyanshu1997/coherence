/* home.js — home page and folder index interactions (search + sort) */

// ── Global search ──────────────────────────────────────────────────────────
function initGlobalSearch() {
  const input      = document.getElementById("global-search");
  const resultsDiv = document.getElementById("search-results");
  const hintEl     = document.getElementById("search-scope-hint");
  const grid       = document.getElementById("folder-grid");   // home page
  const docList    = document.getElementById("doc-list");       // folder index

  if (!input) return;

  // Detect whether we're on the home page or a folder index
  // On a folder index, window.INDEX_FOLDER is set; on home it's undefined
  const isFolder = typeof window.INDEX_FOLDER === "string";
  const folderCards = grid   ? Array.from(grid.querySelectorAll(".folder-card-wrap, .folder-card")) : [];
  const docCards    = docList ? Array.from(docList.querySelectorAll(".doc-card")) : [];

  let debounceTimer = null;

  function highlight(text, query) {
    if (!query) return escHtml(text);
    const re = new RegExp("(" + query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")", "gi");
    return escHtml(text).replace(re, "<mark>$1</mark>");
  }

  function escHtml(s) {
    return s.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;");
  }

  function showFolderResults(q) {
    // Client-side filter on folder cards (home page folders or repo subfolder cards)
    let visible = 0;
    folderCards.forEach(card => {
      const text = card.textContent.toLowerCase();
      const match = !q || text.includes(q.toLowerCase());
      card.style.display = match ? "" : "none";
      if (match) visible++;
    });
    const counter = document.getElementById("folder-count");
    if (counter) counter.textContent = `${visible} folder${visible === 1 ? "" : "s"}`;
    if (hintEl) hintEl.textContent = q ? `${visible} folder${visible === 1 ? "" : "s"} matching` : "";
    resultsDiv.style.display = "none";
  }

  function showDocResults(q) {
    // Client-side filter on doc cards (folder index page)
    let visible = 0;
    docCards.forEach(card => {
      const text = card.textContent.toLowerCase();
      const match = !q || text.includes(q.toLowerCase());
      card.style.display = match ? "" : "none";
      if (match) visible++;
    });
    const counter = document.getElementById("folder-count");
    if (counter) counter.textContent = `${visible} document${visible === 1 ? "" : "s"}`;
    if (hintEl) hintEl.textContent = q ? `${visible} result${visible === 1 ? "" : "s"} in this folder` : "";
    resultsDiv.style.display = "none";
  }

  async function runApiSearch(q) {
    if (hintEl) hintEl.textContent = "Searching…";
    resultsDiv.style.display = "flex";
    resultsDiv.innerHTML = "";
    try {
      const r = await fetch(_API + "/search?q=" + encodeURIComponent(q));
      const d = await r.json();
      if (!r.ok) throw new Error(d.error || "search failed");
      const items = d.results || [];

      if (items.length === 0) {
        resultsDiv.innerHTML = `<div class="search-no-results">No results for "${escHtml(q)}"</div>`;
        if (hintEl) hintEl.textContent = "0 results";
        return;
      }

      resultsDiv.innerHTML = `<div class="search-result-count">${items.length} result${items.length===1?"":"s"} for "${escHtml(q)}"</div>`;
      items.forEach(item => {
        const card = document.createElement("a");
        card.className = "search-result-card";
        card.href = item.url;
        card.innerHTML = `
          <div class="search-result-title">${highlight(item.title, q)}</div>
          <div class="search-result-folder">${escHtml(item.folder || "/")}</div>
          <div class="search-result-snippet">${highlight(item.snippet, q)}</div>`;
        resultsDiv.appendChild(card);
      });
      if (hintEl) hintEl.textContent = `${items.length} result${items.length===1?"":"s"} across all docs`;
    } catch(e) {
      resultsDiv.innerHTML = `<div class="search-no-results">Search error: ${escHtml(e.message)}</div>`;
      if (hintEl) hintEl.textContent = "";
    }
  }

  function onInput() {
    const q = input.value.trim();
    clearTimeout(debounceTimer);

    if (!q) {
      // Clear everything
      if (isFolder) {
        docCards.forEach(c => c.style.display = "");
        const counter = document.getElementById("folder-count");
        if (counter) counter.textContent = `${docCards.length} document${docCards.length===1?"":"s"}`;
      } else {
        folderCards.forEach(c => c.style.display = "");
        const counter = document.getElementById("folder-count");
        if (counter) counter.textContent = `${folderCards.length} folder${folderCards.length===1?"":"s"}`;
      }
      resultsDiv.style.display = "none";
      if (hintEl) hintEl.textContent = "";
      return;
    }

    if (isFolder) {
      // On folder page: client-side filter doc list + also query API for cross-folder hits
      showDocResults(q);
      debounceTimer = setTimeout(() => runApiSearch(q), 350);
    } else {
      // On home page: client-side filter folders + query API for doc content
      showFolderResults(q);
      debounceTimer = setTimeout(() => runApiSearch(q), 350);
    }
  }

  input.addEventListener("input", onInput);
  input.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      input.value = "";
      onInput();
      input.blur();
    }
  });
}

document.addEventListener("DOMContentLoaded", () => {
  // ── Keyboard shortcut: / to focus search ──────────────────────────────
  document.addEventListener("keydown", (e) => {
    const si = document.getElementById("global-search");
    if (e.key === "/" && si && document.activeElement !== si) {
      e.preventDefault();
      si.focus();
    }
  });

  // ── Global search ──────────────────────────────────────────────────────
  initGlobalSearch();

  // ── Sort order toggle (by time vs alphabetical) ────────────────────────
  document.querySelectorAll(".sort-btn[data-sort]").forEach(btn => {
    btn.addEventListener("click", () => {
      const targetId = btn.dataset.target;
      const container = targetId ? document.getElementById(targetId)
                                 : (document.getElementById("folder-grid") || document.getElementById("doc-list"));
      if (!container) return;

      // Toggle active within this button's sort-controls group
      const group = btn.closest(".sort-controls");
      if (group) group.querySelectorAll(".sort-btn").forEach(b => b.classList.remove("active"));
      btn.classList.add("active");

      const by = btn.dataset.sort;
      // Sortable items: folder-card-wrap (repo page) or folder-card (home) or doc-card (leaf folder)
      const items = Array.from(container.children).filter(el =>
        el.classList.contains("folder-card-wrap") ||
        el.classList.contains("folder-card") ||
        el.classList.contains("doc-card")
      );

      items.sort((a, b) => {
        if (by === "alpha") {
          const na = (a.querySelector("[data-name]") || a).dataset.name
                  || a.querySelector(".folder-name, .doc-card-title")?.textContent || "";
          const nb = (b.querySelector("[data-name]") || b).dataset.name
                  || b.querySelector(".folder-name, .doc-card-title")?.textContent || "";
          return na.localeCompare(nb);
        }
        // time: use data-mtime on the card or its inner anchor
        const ma = parseInt((a.querySelector("[data-mtime]") || a).dataset.mtime || 0);
        const mb = parseInt((b.querySelector("[data-mtime]") || b).dataset.mtime || 0);
        return mb - ma;
      });

      items.forEach(el => container.appendChild(el));
    });
  });
});
