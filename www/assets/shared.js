/* shared.js — common UI for home, folder-index, and doc pages */

const _API = "/comment-api";

// ── Folder tree (built from flat list from server) ────────────────────────
async function fetchFolderTree() {
  try {
    const r = await fetch(_API + "/list-folders");
    const d = await r.json();
    // Build recursive tree: { name: "repo-a", children: [ { name: "PROJ-123", children: [] }, ... ] }
    // Root is a synthetic node with children = top-level folders
    const root = { name: "", children: [] };
    for (const path of (d.folders || [])) {
      const parts = path.split("/");
      let node = root;
      for (const part of parts) {
        let child = node.children.find(c => c.name === part);
        if (!child) { child = { name: part, children: [] }; node.children.push(child); }
        node = child;
      }
    }
    return root;
  } catch(e) { return { name: "", children: [] }; }
}

// ── Generic modal helpers ──────────────────────────────────────────────────
function createOverlay(id) {
  const el = document.createElement("div");
  el.id = id;
  el.className = "modal-overlay";
  document.body.appendChild(el);
  el.addEventListener("click", (e) => { if (e.target === el) el.remove(); });
  return el;
}

// ── Directory-browser folder picker (Finder-style column browser) ─────────
// tree: root node from fetchFolderTree() — { name: "", children: [...] }
// forbidPaths: set of path strings that cannot be selected (e.g. folder being moved)
// Returns a widget element. Call widget.getFolder() to read selected path.
function createFolderPicker(tree, initialFolder, forbidPaths) {
  forbidPaths = forbidPaths || new Set();

  // selectedPath is the currently chosen folder (string like "repo-a/PROJ-123")
  // openPath is the path whose children are showing in the rightmost column
  // They differ when a folder with children is highlighted but not "entered"
  let selectedPath = initialFolder || "";
  // openedParts: array of node-names representing the drill-down path currently open in columns
  // e.g. ["repo-a"] means we show root col + repo-a's children col
  let openedParts = [];
  if (initialFolder) {
    // Open all ancestor columns so the initial selection is visible
    const parts = initialFolder.split("/");
    // Open each ancestor (not the leaf itself — it shows as selected in its parent col)
    openedParts = parts.slice(0, -1);
  }

  const wrap = document.createElement("div");
  wrap.className = "fpick";

  function nodeAt(parts) {
    let node = tree;
    for (const p of parts) {
      node = (node.children || []).find(c => c.name === p);
      if (!node) return null;
    }
    return node;
  }

  function pathStr(parts) { return parts.join("/"); }

  function render() {
    // Build columns: root col + one col per opened level
    const cols = [];

    // Column at depth d shows the children of openedParts[0..d-1]
    for (let d = 0; d <= openedParts.length; d++) {
      const parentParts = openedParts.slice(0, d);
      const parentNode  = nodeAt(parentParts);
      if (!parentNode) break;
      const parentPath  = pathStr(parentParts);
      const header      = parentPath ? parentPath : "/ root";

      const children = (parentNode.children || []).slice().sort((a, b) => a.name.localeCompare(b.name));

      // Which child is "active" (highlighted) in this column?
      // It's openedParts[d] if we've drilled further, otherwise the leaf of selectedPath if it lives here
      const activeName = d < openedParts.length
        ? openedParts[d]
        : (selectedPath && selectedPath.startsWith(parentPath ? parentPath + "/" : "") && !selectedPath.slice(parentPath ? parentPath.length + 1 : 0).includes("/")
            ? selectedPath.split("/").pop()
            : "");

      const items = children.map(child => {
        const childPath   = parentPath ? parentPath + "/" + child.name : child.name;
        const hasKids     = child.children && child.children.length > 0;
        const isActive    = child.name === activeName;
        const isForbidden = forbidPaths.has(childPath) || [...forbidPaths].some(f => childPath === f || childPath.startsWith(f + "/"));
        return `<div class="fpick-item${isActive ? " active" : ""}${isForbidden ? " forbidden" : ""}"
          data-depth="${d}" data-name="${child.name}" data-path="${childPath}" data-has-kids="${hasKids ? 1 : 0}">
          <span class="fpick-item-name">${child.name}</span>${hasKids ? '<span class="fpick-arrow">›</span>' : ""}
        </div>`;
      }).join("");

      cols.push(`<div class="fpick-col" data-col="${d}">
        <div class="fpick-col-head">${header}</div>
        ${items}
      </div>`);
    }

    wrap.innerHTML = `
      <div class="fpick-selected">${selectedPath || '<span class="fpick-placeholder">Select a folder…</span>'}</div>
      <div class="fpick-pane">${cols.join("")}</div>`;

    // Wire item clicks
    wrap.querySelectorAll(".fpick-item:not(.forbidden)").forEach(item => {
      item.addEventListener("click", () => {
        const depth   = parseInt(item.dataset.depth);
        const name    = item.dataset.name;
        const path    = item.dataset.path;
        const hasKids = item.dataset.hasKids === "1";

        // Truncate openedParts to depth (close any deeper columns)
        openedParts = openedParts.slice(0, depth);

        if (hasKids) {
          // Drill into this folder — open its children in the next column
          openedParts.push(name);
        }
        // Always select this folder
        selectedPath = path;
        render();
        // Scroll the pane to show the rightmost column
        const pane = wrap.querySelector(".fpick-pane");
        if (pane) setTimeout(() => { pane.scrollLeft = pane.scrollWidth; }, 0);
      });
    });
  }

  render();

  wrap.getFolder = () => selectedPath;
  wrap.setFolder = (path) => {
    selectedPath = path || "";
    openedParts  = path ? path.split("/").slice(0, -1) : [];
    render();
  };

  return wrap;
}

// ── New Folder modal ───────────────────────────────────────────────────────
function initNewFolderModal() {
  const btn = document.getElementById("new-folder-btn");
  if (!btn) return;
  btn.addEventListener("click", () => openNewFolderModal(btn.dataset.folder || window.INDEX_FOLDER || ""));
}

async function openNewFolderModal(parentFolder) {
  const existing = document.getElementById("new-folder-modal");
  if (existing) { existing.remove(); }

  // If called from inside a folder, pre-fill parent and only ask for the subfolder name
  const hasParent = !!parentFolder;
  const placeholder = hasParent ? `e.g. new-folder-name` : `e.g. ai-system or repo-a/PROJ-123`;
  const prefixHtml  = hasParent
    ? `<span class="fpick-col-head" style="display:inline-block;margin-bottom:4px">${parentFolder} /</span> `
    : "";
  const hint = hasParent
    ? `Creates <code>${parentFolder}/<em>name</em></code>`
    : `Use a slash for nested folders`;

  const overlay = createOverlay("new-folder-modal");
  overlay.innerHTML = `
    <div class="modal-box" style="width:400px">
      <div class="modal-title">New Folder</div>
      <div class="modal-field">
        <label class="modal-label">Folder name</label>
        <div style="display:flex;align-items:center;gap:4px">
          ${prefixHtml}<input id="nf-name" class="modal-input" style="flex:1" placeholder="${placeholder}">
        </div>
        <span class="modal-hint" style="font-size:11px;color:var(--text3)">${hint}</span>
      </div>
      <div class="modal-status" id="nf-status"></div>
      <div class="modal-actions">
        <button class="modal-btn-cancel" id="nf-cancel">Cancel</button>
        <button class="modal-btn-primary" id="nf-create">Create</button>
      </div>
    </div>`;

  const nameEl    = overlay.querySelector("#nf-name");
  const status    = overlay.querySelector("#nf-status");
  const createBtn = overlay.querySelector("#nf-create");

  overlay.querySelector("#nf-cancel").addEventListener("click", () => overlay.remove());

  const doCreate = async () => {
    const raw = nameEl.value.trim();
    if (!raw) { status.textContent = "Folder name required."; status.className = "modal-status err"; return; }
    const folder = hasParent ? `${parentFolder}/${raw}` : raw;
    createBtn.disabled = true;
    status.textContent = "Creating…";
    status.className   = "modal-status";
    try {
      const r = await fetch(_API + "/create-folder", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ folder }),
      });
      const d = await r.json();
      if (!r.ok) {
        status.textContent = d.error || "Error creating folder";
        status.className   = "modal-status err";
        createBtn.disabled = false;
        return;
      }
      status.textContent = "Created — refreshing…";
      status.className   = "modal-status ok";
      setTimeout(() => { overlay.remove(); window.location.reload(); }, 800);
    } catch(e) {
      status.textContent = "Error: " + e.message;
      status.className   = "modal-status err";
      createBtn.disabled = false;
    }
  };

  createBtn.addEventListener("click", doCreate);
  nameEl.addEventListener("keydown", (e) => { if (e.key === "Enter") doCreate(); });
  setTimeout(() => nameEl.focus(), 50);
}

// ── New Doc modal ──────────────────────────────────────────────────────────
function initNewDocModal() {
  const btn = document.getElementById("new-doc-btn");
  if (!btn) return;
  btn.addEventListener("click", () => openNewDocModal(btn.dataset.folder || window.INDEX_FOLDER || ""));
}

async function openNewDocModal(defaultFolder) {
  const existing = document.getElementById("new-doc-modal");
  if (existing) { existing.remove(); }

  const tree   = await fetchFolderTree();
  const picker = createFolderPicker(tree, defaultFolder);

  const overlay = createOverlay("new-doc-modal");
  overlay.innerHTML = `
    <div class="modal-box">
      <div class="modal-title">New Document</div>
      <div class="modal-field" id="nd-folder-field">
        <label class="modal-label">Location</label>
      </div>
      <div class="modal-field">
        <label class="modal-label">Title</label>
        <input id="nd-title" class="modal-input" placeholder="My Document Title">
      </div>
      <div class="modal-field">
        <label class="modal-label">Filename <span class="modal-hint">(optional — auto-generated from title)</span></label>
        <input id="nd-filename" class="modal-input" placeholder="plan.html">
      </div>
      <div class="modal-field">
        <label class="modal-label">Content <span class="modal-hint">(markdown)</span></label>
        <textarea id="nd-content" class="modal-textarea" rows="6" placeholder="## Overview&#10;&#10;Write your content here…"></textarea>
      </div>
      <div class="modal-status" id="nd-status"></div>
      <div class="modal-actions">
        <button class="modal-btn-cancel" id="nd-cancel">Cancel</button>
        <button class="modal-btn-primary" id="nd-create">Create</button>
      </div>
    </div>`;

  overlay.querySelector("#nd-folder-field").appendChild(picker);

  const titleEl   = overlay.querySelector("#nd-title");
  const fnameEl   = overlay.querySelector("#nd-filename");
  const contentEl = overlay.querySelector("#nd-content");
  const status    = overlay.querySelector("#nd-status");
  const createBtn = overlay.querySelector("#nd-create");

  overlay.querySelector("#nd-cancel").addEventListener("click", () => overlay.remove());

  createBtn.addEventListener("click", async () => {
    const folder = picker.getFolder();
    const title  = titleEl.value.trim();
    const fname  = fnameEl.value.trim();
    const content = contentEl.value;

    if (!folder) {
      status.textContent = "Please select a folder.";
      status.className   = "modal-status err";
      return;
    }
    if (!title) {
      status.textContent = "Title is required.";
      status.className   = "modal-status err";
      return;
    }

    createBtn.disabled = true;
    status.textContent = "Creating…";
    status.className   = "modal-status";

    try {
      const r = await fetch(_API + "/create-doc", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ folder, title, filename: fname || undefined, content }),
      });
      const d = await r.json();
      if (!r.ok) {
        status.textContent = d.error || "Error creating doc";
        status.className   = "modal-status err";
        createBtn.disabled = false;
        return;
      }
      window.location.href = d.path || d.url || "/";
    } catch(e) {
      status.textContent = "Error: " + e.message;
      status.className   = "modal-status err";
      createBtn.disabled = false;
    }
  });

  setTimeout(() => titleEl.focus(), 50);
}

// ── Upload modal ───────────────────────────────────────────────────────────
function initUploadModal() {
  const btn = document.getElementById("upload-btn");
  if (!btn) return;
  btn.addEventListener("click", () => openUploadModal(btn.dataset.folder || window.INDEX_FOLDER || ""));
}

async function openUploadModal(defaultFolder) {
  const existing = document.getElementById("upload-modal");
  if (existing) { existing.remove(); }

  const tree   = await fetchFolderTree();
  const picker = createFolderPicker(tree, defaultFolder);

  const overlay = createOverlay("upload-modal");
  overlay.innerHTML = `
    <div class="modal-box">
      <div class="modal-title">Upload File</div>
      <div class="modal-field" id="up-folder-field">
        <label class="modal-label">Location <span class="modal-hint" id="up-location-hint">(saved to folder/logs/)</span></label>
      </div>
      <div class="modal-field">
        <label class="modal-label">File</label>
        <input id="up-file" type="file" class="modal-input" style="padding:4px">
      </div>
      <div id="up-preview-field" style="display:none;margin-bottom:8px">
        <img id="up-preview-img" style="max-width:100%;max-height:180px;object-fit:contain;border-radius:4px;border:1px solid var(--border);display:block">
      </div>
      <div class="modal-field" id="up-rename-field">
        <label class="modal-label">Rename to <span class="modal-hint">(optional)</span></label>
        <input id="up-rename" class="modal-input" placeholder="leave blank to keep original name">
      </div>
      <div class="modal-progress" id="up-progress" style="display:none">
        <div class="modal-progress-bar" id="up-progress-bar"></div>
      </div>
      <div class="modal-status" id="up-status"></div>
      <div id="up-markdown-result" style="display:none;margin-top:8px">
        <label class="modal-label">Markdown to embed in doc</label>
        <div style="display:flex;align-items:center;gap:8px;margin-top:4px">
          <input id="up-markdown-input" class="modal-input" readonly style="font-family:var(--font-mono);font-size:12px">
          <button class="modal-btn-primary" id="up-markdown-copy" style="flex-shrink:0">Copy</button>
        </div>
      </div>
      <div class="modal-actions">
        <button class="modal-btn-cancel" id="up-cancel">Cancel</button>
        <button class="modal-btn-primary" id="up-upload">Upload</button>
      </div>
    </div>`;

  overlay.querySelector("#up-folder-field").appendChild(picker);

  const fileEl        = overlay.querySelector("#up-file");
  const renameEl      = overlay.querySelector("#up-rename");
  const renameField   = overlay.querySelector("#up-rename-field");
  const previewField  = overlay.querySelector("#up-preview-field");
  const previewImg    = overlay.querySelector("#up-preview-img");
  const locationHint  = overlay.querySelector("#up-location-hint");
  const status        = overlay.querySelector("#up-status");
  const progWrap      = overlay.querySelector("#up-progress");
  const progBar       = overlay.querySelector("#up-progress-bar");
  const uploadBtn     = overlay.querySelector("#up-upload");
  const markdownResult = overlay.querySelector("#up-markdown-result");
  const markdownInput  = overlay.querySelector("#up-markdown-input");
  const markdownCopy   = overlay.querySelector("#up-markdown-copy");

  overlay.querySelector("#up-cancel").addEventListener("click", () => overlay.remove());

  fileEl.addEventListener("change", () => {
    const file = fileEl.files[0];
    if (file && file.type.startsWith("image/")) {
      const url = URL.createObjectURL(file);
      previewImg.src = url;
      previewField.style.display = "";
      renameField.style.display = "none";
      locationHint.textContent = "(saved to folder/images/)";
    } else {
      previewField.style.display = "none";
      renameField.style.display = "";
      locationHint.textContent = "(saved to folder/logs/)";
    }
    markdownResult.style.display = "none";
  });

  markdownCopy.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(markdownInput.value);
      markdownCopy.textContent = "Copied!";
      setTimeout(() => { markdownCopy.textContent = "Copy"; }, 2000);
    } catch(e) {
      markdownInput.select();
    }
  });

  uploadBtn.addEventListener("click", async () => {
    const folder   = picker.getFolder();
    const file     = fileEl.files[0];
    const isImage  = file && file.type.startsWith("image/");
    if (!folder) { status.textContent = "Please select a folder."; status.className = "modal-status err"; return; }
    if (!file)   { status.textContent = "Choose a file to upload."; status.className = "modal-status err"; return; }

    uploadBtn.disabled     = true;
    progWrap.style.display = "";
    progBar.style.width    = "0%";
    status.textContent     = "Uploading…";
    status.className       = "modal-status";
    markdownResult.style.display = "none";

    const form = new FormData();
    form.append("folder", folder);
    form.append("file", file);
    if (!isImage && renameEl.value.trim()) form.append("rename", renameEl.value.trim());

    const endpoint = isImage ? "/upload-image" : "/upload-file";

    try {
      await new Promise((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.upload.addEventListener("progress", (e) => {
          if (e.lengthComputable)
            progBar.style.width = Math.round(e.loaded / e.total * 100) + "%";
        });
        xhr.addEventListener("load", () => {
          if (xhr.status >= 200 && xhr.status < 300) {
            try {
              const d = JSON.parse(xhr.responseText);
              progBar.style.width = "100%";
              if (isImage && d.markdown) {
                status.textContent = "Uploaded.";
                status.className   = "modal-status ok";
                markdownInput.value = d.markdown;
                markdownResult.style.display = "";
                uploadBtn.disabled = false;
              } else {
                status.textContent = "Uploaded: " + (d.path || file.name);
                status.className   = "modal-status ok";
                setTimeout(() => overlay.remove(), 1500);
              }
            } catch(e) {}
            resolve();
          } else {
            try { reject(new Error(JSON.parse(xhr.responseText).error || xhr.statusText)); }
            catch(e) { reject(new Error(xhr.statusText)); }
          }
        });
        xhr.addEventListener("error", () => reject(new Error("Network error")));
        xhr.open("POST", _API + endpoint);
        xhr.send(form);
      });
    } catch(e) {
      status.textContent = "Upload failed: " + e.message;
      status.className   = "modal-status err";
      uploadBtn.disabled = false;
    }
  });
}

// ── Edit mode (doc pages) ──────────────────────────────────────────────────
function initEditMode() {
  const editBtn = document.getElementById("edit-doc-btn");
  if (!editBtn) return;

  const folder   = window.DOC_FOLDER;
  const filename = window.DOC_FILE;
  if (!folder || !filename) { editBtn.style.display = "none"; return; }

  let editOverlay = null;

  editBtn.addEventListener("click", async () => {
    if (editOverlay) { editOverlay.remove(); editOverlay = null; return; }

    let rawMarkdown = "";
    const rawEl = document.getElementById("doc-raw-markdown");
    if (rawEl) {
      rawMarkdown = rawEl.textContent;
    } else {
      try {
        const r = await fetch(window.location.pathname + "?_nocache=" + Date.now());
        const html = await r.text();
        const m = html.match(/<div class="content">([\s\S]*?)<\/div>\s*<button id="sel-chip"/);
        rawMarkdown = m ? m[1].trim() : "";
      } catch(e) { rawMarkdown = ""; }
    }

    editOverlay = createOverlay("edit-doc-overlay");
    editOverlay.innerHTML = `
      <div class="modal-box modal-box-wide">
        <div class="modal-title">Edit — ${escHtmlAttr(filename)}</div>
        <div class="modal-field">
          <label class="modal-label">Title</label>
          <input id="ed-title" class="modal-input" value="${escHtmlAttr(window.DOC_TITLE || "")}">
        </div>
        <div class="modal-field" style="flex:1;display:flex;flex-direction:column">
          <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px">
            <label class="modal-label" style="margin-bottom:0">Content <span class="modal-hint">${rawEl ? "(markdown)" : "(HTML)"}</span></label>
            <button id="ed-insert-img" style="font-size:12px;padding:3px 10px;border:1px solid var(--border);border-radius:4px;background:var(--surface);color:var(--text3);cursor:pointer;font-family:var(--font)">&#x1F4F7; Insert Image</button>
          </div>
          <input id="ed-img-file" type="file" accept="image/png,image/jpeg,image/gif,image/webp" style="display:none">
          <textarea id="ed-content" class="modal-textarea modal-textarea-tall">${escHtmlContent(rawMarkdown)}</textarea>
        </div>
        <div class="modal-status" id="ed-status"></div>
        <div class="modal-actions">
          <button class="modal-btn-cancel" id="ed-cancel">Cancel</button>
          <button class="modal-btn-primary" id="ed-save">Save</button>
        </div>
      </div>`;

    const titleEl      = editOverlay.querySelector("#ed-title");
    const contentEl    = editOverlay.querySelector("#ed-content");
    const status       = editOverlay.querySelector("#ed-status");
    const saveBtn      = editOverlay.querySelector("#ed-save");
    const insertImgBtn = editOverlay.querySelector("#ed-insert-img");
    const imgFileInput = editOverlay.querySelector("#ed-img-file");

    insertImgBtn.addEventListener("click", () => imgFileInput.click());
    imgFileInput.addEventListener("change", async () => {
      const file = imgFileInput.files[0];
      if (!file) return;
      insertImgBtn.disabled = true;
      insertImgBtn.textContent = "Uploading…";
      const form = new FormData();
      form.append("folder", folder);
      form.append("file", file);
      try {
        const r = await fetch(_API + "/upload-image", { method: "POST", body: form });
        const d = await r.json();
        if (!r.ok) throw new Error(d.error || "upload failed");
        const sel = contentEl.selectionStart;
        contentEl.value = contentEl.value.slice(0, sel) + d.markdown + contentEl.value.slice(contentEl.selectionEnd);
        contentEl.selectionStart = contentEl.selectionEnd = sel + d.markdown.length;
        contentEl.focus();
      } catch(err) {
        alert("Image upload failed: " + err.message);
      } finally {
        insertImgBtn.disabled = false;
        insertImgBtn.textContent = "📷 Insert Image";
        imgFileInput.value = "";
      }
    });

    editOverlay.querySelector("#ed-cancel").addEventListener("click", () => { editOverlay.remove(); editOverlay = null; });
    editOverlay.addEventListener("click", (e) => { if (e.target === editOverlay) { editOverlay.remove(); editOverlay = null; } });

    saveBtn.addEventListener("click", async () => {
      const title   = titleEl.value.trim();
      const content = contentEl.value;
      saveBtn.disabled   = true;
      status.textContent = "Saving…";
      status.className   = "modal-status";
      try {
        const r = await fetch(_API + "/update-doc", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ folder, filename, title, content }),
        });
        const d = await r.json();
        if (!r.ok) {
          status.textContent = d.error || "Save failed";
          status.className   = "modal-status err";
          saveBtn.disabled   = false;
          return;
        }
        status.textContent = "Saved — reloading…";
        status.className   = "modal-status ok";
        setTimeout(() => window.location.reload(), 800);
      } catch(e) {
        status.textContent = "Error: " + e.message;
        status.className   = "modal-status err";
        saveBtn.disabled   = false;
      }
    });

    contentEl.addEventListener("keydown", (e) => {
      if (e.key === "Tab") {
        e.preventDefault();
        const s = contentEl.selectionStart;
        contentEl.value = contentEl.value.slice(0, s) + "  " + contentEl.value.slice(contentEl.selectionEnd);
        contentEl.selectionStart = contentEl.selectionEnd = s + 2;
      }
      if ((e.metaKey || e.ctrlKey) && e.key === "s") { e.preventDefault(); saveBtn.click(); }
    });

    contentEl.addEventListener("paste", async (e) => {
      const items = Array.from(e.clipboardData?.items || []);
      const imgItem = items.find(i => i.type.startsWith("image/"));
      if (!imgItem) return;
      e.preventDefault();
      const file = imgItem.getAsFile();
      if (!file) return;
      const ext = file.type.split("/")[1]?.replace("jpeg", "jpg") || "png";
      const fname = `paste-${Date.now()}.${ext}`;
      const placeholder = "![uploading…]()";
      const sel = contentEl.selectionStart;
      contentEl.value = contentEl.value.slice(0, sel) + placeholder + contentEl.value.slice(contentEl.selectionEnd);
      contentEl.selectionStart = contentEl.selectionEnd = sel + placeholder.length;
      const form = new FormData();
      form.append("folder", folder);
      form.append("file", new File([file], fname, { type: file.type }));
      try {
        const r = await fetch(_API + "/upload-image", { method: "POST", body: form });
        const d = await r.json();
        if (!r.ok) throw new Error(d.error || "upload failed");
        contentEl.value = contentEl.value.replace(placeholder, d.markdown);
      } catch(err) {
        contentEl.value = contentEl.value.replace(placeholder, "");
        alert("Image upload failed: " + err.message);
      }
    });

    setTimeout(() => contentEl.focus(), 50);
  });
}

function escHtmlAttr(s) {
  return String(s).replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}
function escHtmlContent(s) {
  return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// ── Delete folder buttons ─────────────────────────────────────────────────
async function handleDeleteFolder(btn) {
  const folder = btn.dataset.folder;
  const label  = btn.closest(".folder-card-wrap")?.querySelector(".folder-name")?.textContent || folder;
  if (!confirm(`Delete folder "${label}"?\n\nThis permanently removes all docs, logs, and comments.`)) return;
  btn.disabled = true;
  btn.textContent = "⏳";
  try {
    const r = await fetch(_API + "/delete-folder", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ folder }),
    });
    const d = await r.json();
    if (r.ok) {
      const wrap = btn.closest(".folder-card-wrap");
      if (wrap) wrap.remove();
    } else {
      alert("Delete failed: " + (d.error || "unknown error"));
      btn.disabled = false;
      btn.textContent = "🗑";
    }
  } catch(e) {
    alert("Delete failed: " + e.message);
    btn.disabled = false;
    btn.textContent = "🗑";
  }
}

// ── Rename folder ─────────────────────────────────────────────────────────
async function handleRenameFolder(btn) {
  const folder = btn.dataset.folder;
  const label  = btn.closest(".folder-card-wrap")?.querySelector(".folder-name")?.textContent || folder;
  const currentName = folder.includes("/") ? folder.split("/").pop() : folder;

  const overlay = createOverlay("rename-folder-modal");
  overlay.innerHTML = `
    <div class="modal-box" style="width:380px">
      <div class="modal-title">Rename Folder</div>
      <div class="modal-field">
        <label class="modal-label">Current name: <strong>${escHtmlStr(currentName)}</strong></label>
        <input id="rf-name" class="modal-input" value="${escHtmlStr(currentName)}" placeholder="new-folder-name">
      </div>
      <div class="modal-status" id="rf-status"></div>
      <div class="modal-actions">
        <button class="modal-btn-cancel" id="rf-cancel">Cancel</button>
        <button class="modal-btn-primary" id="rf-save">Rename</button>
      </div>
    </div>`;

  const nameEl  = overlay.querySelector("#rf-name");
  const status  = overlay.querySelector("#rf-status");
  const saveBtn = overlay.querySelector("#rf-save");

  overlay.querySelector("#rf-cancel").addEventListener("click", () => overlay.remove());

  const doRename = async () => {
    const new_name = nameEl.value.trim();
    if (!new_name || new_name === currentName) { overlay.remove(); return; }
    saveBtn.disabled   = true;
    status.textContent = "Renaming…";
    status.className   = "modal-status";
    try {
      const r = await fetch(_API + "/rename-folder", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ folder, new_name }),
      });
      const d = await r.json();
      if (!r.ok) {
        status.textContent = d.error || "Rename failed";
        status.className   = "modal-status err";
        saveBtn.disabled   = false;
        return;
      }
      status.textContent = "Renamed — refreshing…";
      status.className   = "modal-status ok";
      setTimeout(() => { overlay.remove(); window.location.href = d.path || "/"; }, 700);
    } catch(e) {
      status.textContent = "Error: " + e.message;
      status.className   = "modal-status err";
      saveBtn.disabled   = false;
    }
  };

  saveBtn.addEventListener("click", doRename);
  nameEl.addEventListener("keydown", (e) => { if (e.key === "Enter") doRename(); if (e.key === "Escape") overlay.remove(); });
  setTimeout(() => { nameEl.select(); nameEl.focus(); }, 50);
}

// ── Delete doc buttons ────────────────────────────────────────────────────
async function handleDeleteDoc(btn) {
  const folder = btn.dataset.folder;
  const file   = btn.dataset.file;
  const label  = btn.closest(".doc-card-wrap")?.querySelector(".doc-card-title")?.textContent || file;
  if (!confirm(`Delete "${label}"?\n\nThis permanently removes the document and its comments.`)) return;
  btn.disabled = true;
  btn.textContent = "⏳";
  try {
    const r = await fetch(_API + "/delete-doc", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ folder, file }),
    });
    const d = await r.json();
    if (r.ok) {
      const wrap = btn.closest(".doc-card-wrap");
      if (wrap) wrap.remove();
    } else {
      alert("Delete failed: " + (d.error || "unknown error"));
      btn.disabled = false;
      btn.textContent = "🗑";
    }
  } catch(e) {
    alert("Delete failed: " + e.message);
    btn.disabled = false;
    btn.textContent = "🗑";
  }
}

// ── Rename doc ────────────────────────────────────────────────────────────
async function handleRenameDoc(btn) {
  const folder      = btn.dataset.folder;
  const file        = btn.dataset.file;
  const currentName = file;

  const overlay = createOverlay("rename-doc-modal");
  overlay.innerHTML = `
    <div class="modal-box" style="width:380px">
      <div class="modal-title">Rename Document</div>
      <div class="modal-field">
        <label class="modal-label">Current name: <strong>${escHtmlStr(currentName)}.html</strong></label>
        <input id="rd-name" class="modal-input" value="${escHtmlStr(currentName)}" placeholder="new-filename">
        <span class="modal-hint" style="font-size:11px;color:var(--text3)">.html is added automatically</span>
      </div>
      <div class="modal-status" id="rd-status"></div>
      <div class="modal-actions">
        <button class="modal-btn-cancel" id="rd-cancel">Cancel</button>
        <button class="modal-btn-primary" id="rd-save">Rename</button>
      </div>
    </div>`;

  const nameEl  = overlay.querySelector("#rd-name");
  const status  = overlay.querySelector("#rd-status");
  const saveBtn = overlay.querySelector("#rd-save");

  overlay.querySelector("#rd-cancel").addEventListener("click", () => overlay.remove());

  const doRename = async () => {
    const new_name = nameEl.value.trim().replace(/\.html$/, "");
    if (!new_name || new_name === currentName) { overlay.remove(); return; }
    saveBtn.disabled   = true;
    status.textContent = "Renaming…";
    status.className   = "modal-status";
    try {
      const r = await fetch(_API + "/rename-doc", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ folder, filename: file + ".html", new_name }),
      });
      const d = await r.json();
      if (!r.ok) {
        status.textContent = d.error || "Rename failed";
        status.className   = "modal-status err";
        saveBtn.disabled   = false;
        return;
      }
      status.textContent = "Renamed — refreshing…";
      status.className   = "modal-status ok";
      setTimeout(() => { overlay.remove(); window.location.reload(); }, 700);
    } catch(e) {
      status.textContent = "Error: " + e.message;
      status.className   = "modal-status err";
      saveBtn.disabled   = false;
    }
  };

  saveBtn.addEventListener("click", doRename);
  nameEl.addEventListener("keydown", (e) => { if (e.key === "Enter") doRename(); if (e.key === "Escape") overlay.remove(); });
  setTimeout(() => { nameEl.select(); nameEl.focus(); }, 50);
}

function escHtmlStr(s) {
  return String(s).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;");
}

// ── Exclude session buttons ────────────────────────────────────────────────
async function handleExcludeSession(btn) {
  const folder     = btn.dataset.folder;
  const session_id = btn.dataset.sessionId;
  const short      = session_id.slice(0, 8);
  if (!confirm(`Remove session ${short}… from this history?\n\nThe session itself is not deleted — it's just hidden from this doc.`)) return;
  btn.disabled = true;
  btn.textContent = "⏳";
  try {
    const r = await fetch(_API + "/exclude-session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ folder, session_id }),
    });
    const d = await r.json();
    if (r.ok) {
      const row = btn.closest(".session-row");
      if (row) {
        row.style.opacity = "0.4";
        row.style.pointerEvents = "none";
        btn.textContent = "✓";
      }
    } else {
      alert("Exclude failed: " + (d.error || "unknown error"));
      btn.disabled = false;
      btn.textContent = "✕";
    }
  } catch(e) {
    alert("Exclude failed: " + e.message);
    btn.disabled = false;
    btn.textContent = "✕";
  }
}

// ── Move folder / doc modal ───────────────────────────────────────────────
async function openMoveModal({ kind, folder, file, displayName }) {
  let tree;
  try {
    tree = await fetchFolderTree();
  } catch(e) {
    alert("Could not load folders: " + e.message);
    return;
  }

  // Folders that cannot be selected as destination
  const forbidPaths = kind === "folder"
    ? new Set([folder, ...(function(){
        const out = [];
        function collect(node, path) {
          const p = path ? path + "/" + node.name : node.name;
          out.push(p);
          (node.children || []).forEach(c => collect(c, p));
        }
        const parts = folder.split("/");
        let node = tree;
        for (const p of parts) { node = (node.children || []).find(c => c.name === p); if (!node) break; }
        if (node) (node.children || []).forEach(c => collect(c, folder));
        return out;
      })()])
    : new Set();

  const overlay = createOverlay("move-modal");
  const box = document.createElement("div");
  box.className = "move-modal-box";
  box.innerHTML = `
    <div class="move-modal-title">Move ${kind === "folder" ? "Folder" : "Document"}</div>
    <div class="move-modal-subtitle">Moving: <code>${escHtmlStr(displayName)}</code></div>
    <div class="move-modal-label">Select destination${kind === "folder" ? " (or root to move to top level)" : ""}</div>
    <div id="mm-picker-wrap"></div>
    ${kind === "folder" ? `<label class="fpick-root-opt"><input type="checkbox" id="mm-root-chk"> Move to root (top level)</label>` : ""}
    <div class="move-modal-status" id="mm-status"></div>
    <div class="move-modal-actions">
      <button class="move-modal-cancel" id="mm-cancel">Cancel</button>
      <button class="move-modal-confirm" id="mm-confirm">Move</button>
    </div>`;
  overlay.appendChild(box);

  // Insert the folder picker
  const pickerWrap = box.querySelector("#mm-picker-wrap");
  const picker = createFolderPicker(tree, "", forbidPaths);
  pickerWrap.appendChild(picker);

  const statusEl   = box.querySelector("#mm-status");
  const confirmBtn = box.querySelector("#mm-confirm");
  const rootChk    = box.querySelector("#mm-root-chk");

  if (rootChk) {
    rootChk.addEventListener("change", () => {
      pickerWrap.style.opacity = rootChk.checked ? "0.4" : "1";
      pickerWrap.style.pointerEvents = rootChk.checked ? "none" : "";
    });
  }

  box.querySelector("#mm-cancel").addEventListener("click", () => overlay.remove());

  confirmBtn.addEventListener("click", async () => {
    const dest = (rootChk && rootChk.checked) ? "" : picker.getFolder();
    if (dest === null || dest === undefined) return;
    // For docs, a destination folder is required
    if (kind === "doc" && !dest) {
      statusEl.textContent = "Please select a destination folder.";
      statusEl.className = "move-modal-status err";
      return;
    }
    confirmBtn.disabled = true;
    statusEl.textContent = "Moving…";
    statusEl.className = "move-modal-status";
    try {
      const endpoint = kind === "folder" ? "/move-folder" : "/move-doc";
      const body = kind === "folder"
        ? { folder, dest_parent: dest }
        : { folder, filename: file + ".html", dest_folder: dest };
      const r = await fetch(_API + endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const d = await r.json();
      if (!r.ok) throw new Error(d.error || "move failed");
      statusEl.textContent = "Moved! Redirecting…";
      statusEl.className = "move-modal-status ok";
      setTimeout(() => { window.location.href = d.path || "/"; }, 800);
    } catch(e) {
      statusEl.textContent = "Error: " + e.message;
      statusEl.className = "move-modal-status err";
      confirmBtn.disabled = false;
    }
  });
}

// ── Share popover ──────────────────────────────────────────────────────────
function initShare() {
  const shareBtn      = document.getElementById("share-btn");
  const sharePop      = document.getElementById("share-popover");
  const generateBtn   = document.getElementById("share-generate-btn");
  const shareResult   = document.getElementById("share-result");
  const shareUrlInput = document.getElementById("share-url-input");
  const shareCopyBtn  = document.getElementById("share-copy-btn");
  const shareExpires  = document.getElementById("share-expires");
  const shareStatus   = document.getElementById("share-status");
  const shareDays     = document.getElementById("share-days");

  if (!shareBtn || !sharePop) return;

  shareBtn.addEventListener("click", (e) => {
    e.stopPropagation();
    const showing = sharePop.style.display === "flex";
    sharePop.style.display = showing ? "none" : "flex";
  });

  document.addEventListener("mousedown", (e) => {
    if (!sharePop.contains(e.target) && e.target !== shareBtn) {
      sharePop.style.display = "none";
    }
  });

  if (generateBtn) {
    generateBtn.addEventListener("click", async () => {
      if (shareStatus) { shareStatus.textContent = "Generating…"; shareStatus.className = "share-status"; }
      if (shareResult) shareResult.style.display = "none";
      try {
        const days = shareDays ? parseInt(shareDays.value) : 30;
        const path = window.location.pathname;
        const r = await fetch("/comment-api/auth/share/create", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ path, days }),
        });
        if (!r.ok) throw new Error("server error " + r.status);
        const data = await r.json();
        if (shareUrlInput) shareUrlInput.value = data.url;
        if (shareExpires)  shareExpires.textContent = "Expires " + data.expires;
        if (shareResult)   shareResult.style.display = "flex";
        if (shareStatus)   shareStatus.textContent = "";
      } catch(e) {
        if (shareStatus) { shareStatus.textContent = "Error: " + e.message; shareStatus.className = "share-status err"; }
      }
    });
  }

  if (shareCopyBtn && shareUrlInput) {
    shareCopyBtn.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(shareUrlInput.value);
        shareCopyBtn.textContent = "Copied!";
        shareCopyBtn.classList.add("copied");
        setTimeout(() => { shareCopyBtn.textContent = "Copy"; shareCopyBtn.classList.remove("copied"); }, 2000);
      } catch(e) {
        shareUrlInput.select();
      }
    });
  }
}

// ── Share token propagation ────────────────────────────────────────────────
// When a page is accessed via ?share=TOKEN, patch all local <a> links so
// navigating within the share (folder → doc, breadcrumb, etc.) keeps the
// token and doesn't hit the password prompt.
function initSharePropagation() {
  const token = new URLSearchParams(window.location.search).get("share");
  if (!token) return;
  document.querySelectorAll("a[href]").forEach(a => {
    const href = a.getAttribute("href");
    if (!href || href.startsWith("http") || href.startsWith("//")
        || href.startsWith("mailto") || href.startsWith("#")
        || href.startsWith("/auth/")) return;
    try {
      const url = new URL(a.href, window.location.href);
      url.searchParams.set("share", token);
      a.href = url.toString();
    } catch(e) {}
  });
}

// ── Auto-init on DOM ready ─────────────────────────────────────────────────
document.addEventListener("DOMContentLoaded", () => {
  initNewFolderModal();
  initNewDocModal();
  initUploadModal();
  initEditMode();
  initShare();
  initSharePropagation();

  // Wire delete folder buttons (present on home page and repo index pages)
  document.querySelectorAll(".folder-delete-btn").forEach(btn => {
    btn.addEventListener("click", (e) => { e.preventDefault(); e.stopPropagation(); handleDeleteFolder(btn); });
  });

  // Wire rename folder buttons
  document.querySelectorAll(".folder-rename-btn").forEach(btn => {
    btn.addEventListener("click", (e) => { e.preventDefault(); e.stopPropagation(); handleRenameFolder(btn); });
  });

  // Wire move folder buttons
  document.querySelectorAll(".folder-move-btn").forEach(btn => {
    btn.addEventListener("click", (e) => {
      e.preventDefault(); e.stopPropagation();
      const folder = btn.dataset.folder;
      openMoveModal({ kind: "folder", folder, displayName: folder.split("/").pop() });
    });
  });

  // Wire doc delete and rename buttons (present on folder index pages)
  document.querySelectorAll(".doc-delete-btn").forEach(btn => {
    btn.addEventListener("click", (e) => { e.preventDefault(); e.stopPropagation(); handleDeleteDoc(btn); });
  });
  document.querySelectorAll(".doc-rename-btn").forEach(btn => {
    btn.addEventListener("click", (e) => { e.preventDefault(); e.stopPropagation(); handleRenameDoc(btn); });
  });

  // Wire move doc buttons
  document.querySelectorAll(".doc-move-btn").forEach(btn => {
    btn.addEventListener("click", (e) => {
      e.preventDefault(); e.stopPropagation();
      const folder = btn.dataset.folder;
      const file   = btn.dataset.file;
      openMoveModal({ kind: "doc", folder, file, displayName: file + ".html" });
    });
  });

  // Wire session exclude buttons (present on session-history docs)
  document.querySelectorAll(".session-exclude-btn").forEach(btn => {
    btn.addEventListener("click", (e) => { e.preventDefault(); e.stopPropagation(); handleExcludeSession(btn); });
  });
});
