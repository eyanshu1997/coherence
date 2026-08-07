/* doc.js — shared logic for all coherence pages */

const FOLDER = window.DOC_FOLDER;
const FILE   = window.DOC_FILE;
const API    = "/comment-api";
const COMMENTS_ENABLED = !!(FOLDER && FILE);

// ── utilities ──────────────────────────────────────────────────────────────
function fmtTs(ts) {
  try { return new Date(ts).toLocaleString(); } catch(e) { return ts; }
}
function escHtml(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// ── scrollspy (IntersectionObserver) ──────────────────────────────────────
function initScrollspy() {
  const sidebar = document.getElementById("doc-sidebar");
  if (!sidebar) return;
  const section = document.getElementById("sidebar-nav");
  if (!section) return;

  const headings = Array.from(document.querySelectorAll(".content h2, .content h3"));
  if (!headings.length) {
    sidebar.style.display = "none";
    return;
  }

  const links = headings.map((h, i) => {
    if (!h.id) h.id = "h-" + i;
    const a = document.createElement("a");
    a.href  = "#" + h.id;
    a.className = h.tagName === "H3" ? "sidebar-link sidebar-link-sub" : "sidebar-link";
    a.textContent = h.textContent;
    section.appendChild(a);
    return { el: h, link: a };
  });

  if (links.length) links[0].link.classList.add("active");

  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      const idx = links.findIndex(l => l.el === entry.target);
      if (idx < 0) continue;
      links.forEach(l => l.link.classList.remove("active"));
      links[idx].link.classList.add("active");
    }
  }, { rootMargin: "-8% 0px -85% 0px", threshold: 0 });

  links.forEach(l => observer.observe(l.el));
}

// ── copy buttons ──────────────────────────────────────────────────────────
function initCopyButtons() {
  document.querySelectorAll("pre").forEach(pre => {
    if (pre.querySelector(".copy-btn")) return;
    const btn = document.createElement("button");
    btn.className   = "copy-btn";
    btn.textContent = "Copy";
    btn.addEventListener("click", async () => {
      const code = pre.querySelector("code");
      const text = code ? code.textContent : pre.textContent;
      try {
        await navigator.clipboard.writeText(text);
        btn.textContent = "Copied!";
        btn.classList.add("copied");
        setTimeout(() => { btn.textContent = "Copy"; btn.classList.remove("copied"); }, 2000);
      } catch(e) {
        btn.textContent = "Error";
        setTimeout(() => { btn.textContent = "Copy"; }, 2000);
      }
    });
    pre.appendChild(btn);
  });
}

// ── comment API ───────────────────────────────────────────────────────────
async function postComment(text, quote, range) {
  if (!COMMENTS_ENABLED) return;
  const body = { folder: FOLDER, file: FILE, text };
  if (quote) body.quote = quote;
  if (range) body.range = range;
  const r = await fetch(`${API}/comment`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error("server error " + r.status);
}

async function postThreadReply(parentTs, text) {
  if (!COMMENTS_ENABLED) return;
  const r = await fetch(`${API}/add-reply`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ folder: FOLDER, file: FILE, ts: parentTs, text }),
  });
  if (!r.ok) throw new Error("server error " + r.status);
}

async function deleteComment(ts) {
  if (!COMMENTS_ENABLED) return;
  const r = await fetch(`${API}/delete-comment`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ folder: FOLDER, file: FILE, ts }),
  });
  if (!r.ok) throw new Error("server error " + r.status);
}

// ── inline highlight marks ─────────────────────────────────────────────────
// Map from comment ts → mark element in the doc body.
const marksByTs = {};

function clearMarks() {
  Array.from(document.querySelectorAll("mark.inline-comment-mark")).forEach(mark => {
    mark.replaceWith(...Array.from(mark.childNodes));
  });
  Object.keys(marksByTs).forEach(k => delete marksByTs[k]);
  // Rejoin text nodes split by surroundContents so paths are valid on the next render pass.
  const content = document.querySelector(".content");
  if (content) content.normalize();
}

// ── Range path serialization ──────────────────────────────────────────────
// A "path" is an array of childNode indices from root down to the target node.
// This survives any DOM structure — code blocks, headers, nested elements.

function nodeToPath(root, node) {
  const path = [];
  let cur = node;
  while (cur && cur !== root) {
    const parent = cur.parentNode;
    if (!parent) return null;
    path.unshift(Array.from(parent.childNodes).indexOf(cur));
    cur = parent;
  }
  return cur === root ? path : null;
}

function pathToNode(root, path) {
  let cur = root;
  for (const idx of path) {
    if (!cur.childNodes[idx]) return null;
    cur = cur.childNodes[idx];
  }
  return cur;
}

function lastTextNodeBefore(root, container, offset) {
  // Find the last text node that is strictly before container[offset] in document order.
  // We do this by collecting all text nodes and finding the last one inside the range.
  const all = [];
  const tw = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  let n;
  while ((n = tw.nextNode())) all.push(n);
  // The boundary position: create a collapsed range at container[offset]
  const boundary = document.createRange();
  boundary.setStart(container, offset);
  boundary.collapse(true);
  // Walk backward through text nodes to find the last one that ends at or before boundary
  for (let i = all.length - 1; i >= 0; i--) {
    const tn = all[i];
    const cmp = boundary.compareBoundaryPoints(Range.START_TO_START,
      (() => { const r = document.createRange(); r.setStart(tn, 0); return r; })());
    if (cmp >= 0) return tn;
  }
  return null;
}

// Returns the nearest block-level ancestor of node within root, or root if none.
const BLOCK_TAGS = new Set(['P','DIV','LI','H1','H2','H3','H4','H5','H6','TD','TH','BLOCKQUOTE','PRE','DT','DD']);
function closestBlock(node, root) {
  let n = node;
  while (n && n !== root) {
    if (BLOCK_TAGS.has(n.nodeName)) return n;
    n = n.parentNode;
  }
  return root;
}

function serializeRange(sel) {
  const content = document.querySelector(".content");
  if (!content || !sel || sel.rangeCount === 0) return null;
  const r = sel.getRangeAt(0);
  if (!content.contains(r.startContainer) || !content.contains(r.endContainer)) return null;

  let startNode = r.startContainer, startOffset = r.startOffset;
  let endNode   = r.endContainer,   endOffset   = r.endOffset;

  // If start is not a text node, find the first text node inside it at the offset
  if (startNode.nodeType !== Node.TEXT_NODE) {
    const child = startNode.childNodes[startOffset];
    const tw = document.createTreeWalker(child || startNode, NodeFilter.SHOW_TEXT);
    const first = tw.nextNode();
    if (!first) return null;
    startNode = first; startOffset = 0;
  }

  // If end is not a text node, walk back to the last text node before the boundary
  if (endNode.nodeType !== Node.TEXT_NODE) {
    const prev = lastTextNodeBefore(content, endNode, endOffset);
    if (!prev) return null;
    endNode = prev; endOffset = prev.nodeValue.length;
  }

  // If end falls outside start's block ancestor, snap it back to the end of that block.
  // This prevents storing a cross-element range that surroundContents cannot wrap.
  const startBlock = closestBlock(startNode, content);
  if (!startBlock.contains(endNode)) {
    const tw = document.createTreeWalker(startBlock, NodeFilter.SHOW_TEXT);
    let last = null, n;
    while ((n = tw.nextNode())) last = n;
    if (!last) return null;
    endNode = last; endOffset = last.nodeValue.length;
  }

  const sc = nodeToPath(content, startNode);
  const ec = nodeToPath(content, endNode);
  if (!sc || !ec) return null;
  return { sc, so: startOffset, ec, eo: endOffset };
}

function restoreRange(rangeData) {
  const content = document.querySelector(".content");
  if (!content || !rangeData) return null;
  let startNode = pathToNode(content, rangeData.sc);
  let endNode   = pathToNode(content, rangeData.ec);
  if (!startNode || !endNode) return null;
  let so = rangeData.so, eo = rangeData.eo;
  // Snap non-text endpoints to nearest text nodes
  if (startNode.nodeType !== Node.TEXT_NODE) {
    const tw = document.createTreeWalker(startNode, NodeFilter.SHOW_TEXT);
    startNode = tw.nextNode();
    so = 0;
    if (!startNode) return null;
  }
  if (endNode.nodeType !== Node.TEXT_NODE) {
    const prev = lastTextNodeBefore(content, endNode, eo);
    if (!prev) return null;
    endNode = prev; eo = prev.nodeValue.length;
  }
  try {
    const r = document.createRange();
    r.setStart(startNode, so);
    r.setEnd(endNode, eo);
    if (r.collapsed) return null;
    return r;
  } catch(e) { return null; }
}

function applyMark(comment, preResolvedRange) {
  // Only apply marks for comments that have a stored range path.
  // Old quote-only comments (no range) fall through to the bottom list instead.
  if (!comment.range) return;
  const range = preResolvedRange !== undefined ? preResolvedRange : restoreRange(comment.range);
  if (!range) return;

  const mark = document.createElement("mark");
  mark.className = "inline-comment-mark";
  mark.dataset.commentTs = comment.ts;
  try {
    range.surroundContents(mark);
  } catch(e) {
    return; // range crosses element boundaries we can't safely wrap — skip mark
  }
  marksByTs[comment.ts] = [mark];
  mark.addEventListener("click", (e) => {
    e.stopPropagation();
    openInlinePanel(comment.ts);
  });
}

// ── inline comment panel ───────────────────────────────────────────────────
let _allComments = [];

function openInlinePanel(ts) {
  closeInlinePanel();
  const comment = _allComments.find(c => c.ts === ts);
  if (!comment) return;
  const marks = marksByTs[ts];
  const anchor = marks && marks[0];

  const panel = document.createElement("div");
  panel.id = "inline-comment-panel";
  panel.className = "inline-comment-panel";

  const isHandled = comment.handled || comment.acknowledged;
  const handledTs = comment.reply_ts || comment.ack_ts || "";
  const badge = isHandled
    ? `<span class="comment-ack-badge" title="Handled${handledTs ? ' on ' + fmtTs(handledTs) : ''}">✓ Handled</span>`
    : "";
  const authorHtml = comment.author && comment.author !== "anonymous"
    ? `<span class="comment-author">${escHtml(comment.author)}</span>` : "";

  // Claude reply bubble (existing /reply-comment flow)
  const claudeReplyHtml = comment.reply
    ? `<div class="comment-reply">` +
        `<span class="comment-reply-label">Claude:</span> ` +
        `<span class="comment-reply-text">${escHtml(comment.reply)}</span>` +
      `</div>`
    : "";

  // Thread replies (new /add-reply flow)
  const repliesHtml = (comment.replies || []).map(rep => {
    const repAuthor = rep.author && rep.author !== "anonymous"
      ? `<span class="comment-author">${escHtml(rep.author)}</span>` : "";
    return `<div class="thread-reply">` +
      `<div class="comment-ts">${repAuthor}${fmtTs(rep.ts)}</div>` +
      `<div class="comment-text">${escHtml(rep.text)}</div>` +
    `</div>`;
  }).join("");

  panel.innerHTML =
    `<div class="icp-header">` +
      `<div class="icp-quote">${escHtml(comment.quote || "")}</div>` +
      `<div class="icp-header-actions">` +
        `<button class="icp-delete" id="icp-delete-btn" title="Delete comment">🗑</button>` +
        `<button class="icp-close" id="icp-close-btn">✕</button>` +
      `</div>` +
    `</div>` +
    `<div class="comment-item-header">` +
      `<div class="comment-ts">${authorHtml}${fmtTs(comment.ts)}</div>` +
      badge +
    `</div>` +
    `<div class="comment-text">${escHtml(comment.text)}</div>` +
    claudeReplyHtml +
    (repliesHtml ? `<div class="thread-replies">${repliesHtml}</div>` : "") +
    `<div class="thread-reply-form">` +
      `<textarea class="thread-reply-input" id="icp-reply-input" placeholder="Reply to this thread…" rows="2"></textarea>` +
      `<div class="thread-reply-actions">` +
        `<button class="thread-reply-submit" id="icp-reply-submit">Reply</button>` +
        `<span class="comment-save-status" id="icp-reply-status"></span>` +
      `</div>` +
    `</div>`;

  document.body.appendChild(panel);

  // position near anchor mark
  if (anchor) {
    const rect = anchor.getBoundingClientRect();
    const pw = 320;
    let left = rect.left + window.scrollX;
    let top  = rect.bottom + window.scrollY + 8;
    if (left + pw > window.innerWidth - 8) left = window.innerWidth - pw - 8;
    panel.style.left = `${Math.max(8, left)}px`;
    panel.style.top  = `${top}px`;
  }

  document.getElementById("icp-close-btn").addEventListener("click", closeInlinePanel);

  document.getElementById("icp-delete-btn").addEventListener("click", async () => {
    if (!confirm("Delete this comment and its entire thread?")) return;
    try {
      await deleteComment(ts);
      closeInlinePanel();
      await loadComments();
    } catch(e) {
      alert("Delete failed: " + e.message);
    }
  });

  const replyInput  = document.getElementById("icp-reply-input");
  const replySubmit = document.getElementById("icp-reply-submit");
  const replyStatus = document.getElementById("icp-reply-status");

  async function submitThreadReply() {
    const text = replyInput.value.trim();
    if (!text) return;
    replyStatus.textContent = "Saving…";
    replyStatus.className = "comment-save-status";
    try {
      await postThreadReply(ts, text);
      replyStatus.textContent = "Saved ✓";
      replyStatus.className = "comment-save-status ok";
      await loadComments();
      setTimeout(closeInlinePanel, 600);
    } catch(e) {
      replyStatus.textContent = "Error";
      replyStatus.className = "comment-save-status err";
    }
  }

  replySubmit.addEventListener("click", submitThreadReply);
  replyInput.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") { e.preventDefault(); submitThreadReply(); }
    if (e.key === "Escape") { e.preventDefault(); closeInlinePanel(); }
  });

  // close on outside click
  setTimeout(() => {
    document.addEventListener("mousedown", icpOutsideClick);
  }, 0);
}

function icpOutsideClick(e) {
  const panel = document.getElementById("inline-comment-panel");
  if (panel && !panel.contains(e.target) && !e.target.closest("mark.inline-comment-mark")) {
    closeInlinePanel();
  }
}

function closeInlinePanel() {
  const panel = document.getElementById("inline-comment-panel");
  if (panel) panel.remove();
  document.removeEventListener("mousedown", icpOutsideClick);
}

// ── build comment item HTML (shared for doc-level and stale-inline) ──────────
function buildCommentItemHtml(c, opts) {
  const stale = opts && opts.stale; // inline comment whose range no longer resolves
  const authorHtml = c.author && c.author !== "anonymous"
    ? `<span class="comment-author">${escHtml(c.author)}</span>` : "";
  const isHandled = c.handled || c.acknowledged;
  const handledTs = c.reply_ts || c.ack_ts || "";
  const badge = isHandled
    ? `<span class="comment-ack-badge" title="Handled by Claude${handledTs ? ' on ' + fmtTs(handledTs) : ''}">✓ Handled</span>`
    : "";
  const itemClass = (isHandled ? "comment-item comment-item-acked" : "comment-item") +
                    (stale ? " comment-item-stale" : "");

  const quoteHtml = (c.quote && (stale || !c.range))
    ? `<div class="comment-quote">${escHtml(c.quote)}</div>`
    : "";

  const claudeReplyHtml = c.reply
    ? `<div class="comment-reply">` +
        `<span class="comment-reply-label">Claude:</span> ` +
        `<span class="comment-reply-text">${escHtml(c.reply)}</span>` +
      `</div>`
    : "";

  const repliesHtml = (c.replies || []).map(rep => {
    const repAuthor = rep.author && rep.author !== "anonymous"
      ? `<span class="comment-author">${escHtml(rep.author)}</span>` : "";
    return `<div class="thread-reply">` +
      `<div class="comment-ts">${repAuthor}${fmtTs(rep.ts)}</div>` +
      `<div class="comment-text">${escHtml(rep.text)}</div>` +
    `</div>`;
  }).join("");

  const ts = escHtml(c.ts);
  return (
    `<div class="${itemClass}" data-comment-ts="${ts}">` +
      `<div class="comment-item-header">` +
        `<div class="comment-ts">${authorHtml}${fmtTs(c.ts)}</div>` +
        `<div class="comment-item-actions">` +
          badge +
          `<button class="comment-delete-btn" data-parent-ts="${ts}" title="Delete comment">✕</button>` +
        `</div>` +
      `</div>` +
      quoteHtml +
      `<div class="comment-text">${escHtml(c.text)}</div>` +
      claudeReplyHtml +
      (repliesHtml ? `<div class="thread-replies">${repliesHtml}</div>` : "") +
      `<div class="thread-reply-form">` +
        `<textarea class="thread-reply-input" data-parent-ts="${ts}" placeholder="Reply…" rows="2"></textarea>` +
        `<div class="thread-reply-actions">` +
          `<button class="thread-reply-submit" data-parent-ts="${ts}">Reply</button>` +
          `<span class="comment-save-status thread-reply-status"></span>` +
        `</div>` +
      `</div>` +
    `</div>`
  );
}

// ── render comment list ────────────────────────────────────────────────────
function renderComments(comments) {
  _allComments = comments || [];
  clearMarks();

  // Resolve all ranges before mutating the DOM (surroundContents splits text nodes and shifts indices).
  // Phase 1: resolve; Phase 2: apply — so each applyMark sees the pristine DOM.
  const resolved = _allComments.map(c => c.range ? restoreRange(c.range) : null);
  _allComments.forEach((c, i) => {
    if (c.range) { try { applyMark(c, resolved[i]); } catch(e) { console.warn("inline mark failed", e); } }
  });

  const list = document.getElementById("comment-list");
  if (!list) return;

  // Bottom list = doc-level (no range) + stale inline (range stored but unresolvable after doc update)
  const bottomList = _allComments.filter((c, i) => !c.range || !resolved[i]);

  if (!_allComments.length) {
    list.innerHTML = '<p class="comments-empty">No comments yet.</p>';
    return;
  }
  if (!bottomList.length) {
    list.innerHTML = '';
    return;
  }

  list.innerHTML = bottomList.map((c, i) => {
    const stale = !!(c.range && !resolved[_allComments.indexOf(c)]);
    return buildCommentItemHtml(c, { stale });
  }).join("");

  // wire reply buttons
  list.querySelectorAll(".thread-reply-submit").forEach(btn => {
    btn.addEventListener("click", async () => {
      const parentTs = btn.dataset.parentTs;
      const item = btn.closest(".comment-item");
      const input = item && item.querySelector(".thread-reply-input");
      const status = item && item.querySelector(".thread-reply-status");
      if (!input || !parentTs) return;
      const text = input.value.trim();
      if (!text) return;
      if (status) { status.textContent = "Saving…"; status.className = "comment-save-status thread-reply-status"; }
      try {
        await postThreadReply(parentTs, text);
        if (status) { status.textContent = "Saved ✓"; status.className = "comment-save-status thread-reply-status ok"; }
        input.value = "";
        await loadComments();
      } catch(e) {
        if (status) { status.textContent = "Error"; status.className = "comment-save-status thread-reply-status err"; }
      }
    });
  });

  // wire delete buttons
  list.querySelectorAll(".comment-delete-btn").forEach(btn => {
    btn.addEventListener("click", async (e) => {
      e.stopPropagation();
      const ts = btn.dataset.parentTs;
      if (!ts) return;
      if (!confirm("Delete this comment and its entire thread?")) return;
      try {
        await deleteComment(ts);
        await loadComments();
      } catch(e) {
        alert("Delete failed: " + e.message);
      }
    });
  });
}

async function loadComments() {
  if (!COMMENTS_ENABLED) return;
  try {
    const r = await fetch(
      `${API}/comments?folder=${encodeURIComponent(FOLDER)}&file=${encodeURIComponent(FILE)}`
    );
    renderComments(await r.json());
  } catch(e) {
    const list = document.getElementById("comment-list");
    if (list) list.innerHTML = '<p class="comments-error">Comment server not available.</p>';
  }
}

// ── bottom textarea submit ─────────────────────────────────────────────────
async function submitComment() {
  const input  = document.getElementById("comment-input");
  const status = document.getElementById("comment-status");
  if (!input || !status) return;
  const text = input.value.trim();
  if (!text) return;
  status.textContent = "Saving…";
  status.className   = "comment-save-status";
  try {
    await postComment(text, null);
    input.value        = "";
    status.textContent = "Saved.";
    status.className   = "comment-save-status ok";
    await loadComments();
    setTimeout(() => { status.textContent = ""; }, 3000);
  } catch(e) {
    status.textContent = "Error — comment server unavailable.";
    status.className   = "comment-save-status err";
  }
}

// ── selection → chip → popover ────────────────────────────────────────────
const chip     = document.getElementById("sel-chip");
const popover  = document.getElementById("sel-popover");
const popQuote = document.getElementById("pop-quote");
const popInput = document.getElementById("pop-input");
const popStat  = document.getElementById("pop-status");

let pendingQuote = "";
let pendingRange = null; // serialized Range path

function popClose() {
  if (popover) popover.style.display = "none";
  if (chip)    chip.style.display    = "none";
  if (popInput) popInput.value = "";
  if (popStat)  { popStat.textContent = "⌘↵ to save"; popStat.className = ""; }
  pendingQuote = "";
  pendingRange = null;
}

document.addEventListener("mouseup", (e) => {
  if (!COMMENTS_ENABLED) return;
  if (popover && (popover.contains(e.target) || e.target === chip)) return;
  setTimeout(() => {
    const sel  = window.getSelection();
    const text = sel ? sel.toString().trim() : "";
    if (!text || text.length < 3) {
      if (chip && !(popover && popover.contains(e.target))) chip.style.display = "none";
      return;
    }
    const range = sel.getRangeAt(0);
    const rect  = range.getBoundingClientRect();
    const top   = rect.bottom + 6;
    const left  = Math.max(8, Math.min(
      rect.left + rect.width / 2 - 60,
      window.innerWidth - 180
    ));
    if (chip) {
      chip.style.top     = `${top}px`;
      chip.style.left    = `${left}px`;
      chip.style.display = "flex";
    }
    pendingQuote = text.length > 120 ? text.slice(0, 117) + "…" : text;
    pendingRange = serializeRange(sel);
  }, 10);
});

if (chip) {
  chip.addEventListener("click", (e) => {
    e.stopPropagation();
    chip.style.display = "none";
    if (!popover) return;
    const cx = parseFloat(chip.style.left) || 100;
    const cy = parseFloat(chip.style.top)  || 100;
    const pw = 320, ph = 190;
    const left = Math.max(8, Math.min(cx, window.innerWidth  - pw - 8));
    const top  = cy - ph - 8 > 8 ? cy - ph - 8 : cy + 36;
    popover.style.left    = `${left}px`;
    popover.style.top     = `${top}px`;
    popover.style.display = "flex";
    if (popQuote) popQuote.textContent = pendingQuote;
    if (popInput) { popInput.value = ""; popInput.focus(); }
    if (popStat)  { popStat.textContent = "⌘↵ to save"; popStat.className = ""; }
  });
}

document.addEventListener("mousedown", (e) => {
  if (popover && !popover.contains(e.target) && e.target !== chip) {
    popover.style.display = "none";
  }
});

if (popInput) {
  popInput.addEventListener("keydown", (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") { e.preventDefault(); popSave(); }
    if (e.key === "Escape") { e.preventDefault(); popClose(); }
  });
}

const popSaveBtn   = document.getElementById("pop-save-btn");
const popCancelBtn = document.getElementById("pop-cancel-btn");
if (popSaveBtn)   popSaveBtn.addEventListener("click",   popSave);
if (popCancelBtn) popCancelBtn.addEventListener("click", popClose);

async function popSave() {
  if (!popInput) return;
  const text = popInput.value.trim();
  if (!text) return;
  if (popStat) { popStat.textContent = "Saving…"; popStat.className = ""; }
  try {
    await postComment(text, pendingQuote, pendingRange);
    if (popStat) { popStat.textContent = "Saved ✓"; popStat.className = "ok"; }
    await loadComments();
    setTimeout(popClose, 800);
  } catch(e) {
    if (popStat) { popStat.textContent = "Error"; popStat.className = "err"; }
  }
}

// ── mermaid zoom modal ────────────────────────────────────────────────────
function openMermaidModal(svgEl) {
  const existing = document.getElementById("mermaid-zoom-modal");
  if (existing) existing.remove();

  const modal = document.createElement("div");
  modal.id = "mermaid-zoom-modal";

  const inner = document.createElement("div");
  inner.className = "mzm-inner";

  // Clone SVG and remove fixed width/height so it fills the modal
  const clone = svgEl.cloneNode(true);
  clone.removeAttribute("width");
  clone.removeAttribute("height");
  clone.style.cssText = "width:100%;height:100%;";

  // Ensure viewBox is set (mermaid usually sets it)
  if (!clone.getAttribute("viewBox")) {
    const w = svgEl.getBoundingClientRect().width || 800;
    const h = svgEl.getBoundingClientRect().height || 600;
    clone.setAttribute("viewBox", `0 0 ${w} ${h}`);
  }

  inner.appendChild(clone);
  modal.appendChild(inner);

  // Close button
  const closeBtn = document.createElement("button");
  closeBtn.className = "mzm-close";
  closeBtn.innerHTML = "✕";
  closeBtn.title = "Close (Esc)";
  closeBtn.addEventListener("click", () => modal.remove());
  modal.appendChild(closeBtn);

  // Hint
  const hint = document.createElement("div");
  hint.className = "mzm-hint";
  hint.textContent = "Scroll to zoom · Drag to pan · Esc to close";
  modal.appendChild(hint);

  document.body.appendChild(modal);

  // Close on backdrop click
  modal.addEventListener("click", e => { if (e.target === modal) modal.remove(); });
  // Close on Esc
  const onKey = e => { if (e.key === "Escape") { modal.remove(); document.removeEventListener("keydown", onKey); } };
  document.addEventListener("keydown", onKey);

  // ── Pan + zoom on the SVG ────────────────────────────────────────────
  const svg = clone;
  let vb = svg.viewBox.baseVal;
  // snapshot initial viewBox
  let vbX = vb.x, vbY = vb.y, vbW = vb.width, vbH = vb.height;

  function applyVB() {
    svg.setAttribute("viewBox", `${vbX} ${vbY} ${vbW} ${vbH}`);
  }

  // Zoom via scroll wheel
  svg.addEventListener("wheel", e => {
    e.preventDefault();
    const factor = e.deltaY > 0 ? 1.12 : 0.89;
    // Zoom toward mouse position
    const rect = svg.getBoundingClientRect();
    const mx = ((e.clientX - rect.left) / rect.width)  * vbW + vbX;
    const my = ((e.clientY - rect.top)  / rect.height) * vbH + vbY;
    vbX = mx - (mx - vbX) * factor;
    vbY = my - (my - vbY) * factor;
    vbW *= factor;
    vbH *= factor;
    applyVB();
  }, { passive: false });

  // Pan via drag
  let dragging = false, dragStart = null;
  svg.addEventListener("mousedown", e => {
    dragging = true;
    dragStart = { x: e.clientX, y: e.clientY, vbX, vbY };
    svg.style.cursor = "grabbing";
    e.preventDefault();
  });
  window.addEventListener("mousemove", e => {
    if (!dragging) return;
    const rect = svg.getBoundingClientRect();
    const dx = (e.clientX - dragStart.x) / rect.width  * vbW;
    const dy = (e.clientY - dragStart.y) / rect.height * vbH;
    vbX = dragStart.vbX - dx;
    vbY = dragStart.vbY - dy;
    applyVB();
  });
  window.addEventListener("mouseup", () => {
    if (!dragging) return;
    dragging = false;
    svg.style.cursor = "grab";
  });
  svg.style.cursor = "grab";
}

// ── mermaid diagrams ──────────────────────────────────────────────────────
async function initMermaid() {
  const blocks = document.querySelectorAll("pre code.language-mermaid");
  if (!blocks.length) return;

  // Wait up to 3s for the mermaid module (loaded async via type=module)
  let mermaid = window._mermaid;
  if (!mermaid) {
    for (let i = 0; i < 30; i++) {
      await new Promise(r => setTimeout(r, 100));
      mermaid = window._mermaid;
      if (mermaid) break;
    }
  }
  if (!mermaid) return;

  for (const code of blocks) {
    const pre = code.closest("pre");
    if (!pre) continue;
    const src = code.textContent;
    const id  = "mermaid-" + Math.random().toString(36).slice(2);
    try {
      const { svg } = await mermaid.render(id, src);
      const wrapper = document.createElement("div");
      wrapper.className = "mermaid-diagram";
      wrapper.innerHTML = svg;
      // Add zoom button
      const zoomBtn = document.createElement("button");
      zoomBtn.className = "mermaid-zoom-btn";
      zoomBtn.title = "View full size";
      zoomBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 16 16" fill="none"><path d="M6.5 11.5a5 5 0 1 0 0-10 5 5 0 0 0 0 10zm0 0L13 13" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/><path d="M4.5 6.5h4M6.5 4.5v4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>';
      zoomBtn.addEventListener("click", () => openMermaidModal(wrapper.querySelector("svg")));
      wrapper.appendChild(zoomBtn);
      pre.replaceWith(wrapper);
    } catch(e) {
      // leave as code block if render fails
      console.warn("mermaid render failed:", e);
    }
  }
}

// ── print / save-as-PDF mode ──────────────────────────────────────────────
function initPrintMode() {
  // Only applicable on doc pages (not folder/index pages)
  if (!document.querySelector(".content")) return;

  let btn = document.getElementById("print-doc-btn");
  // Inject button dynamically if the template predates print mode
  if (!btn) {
    const shareBtn = document.getElementById("share-btn");
    if (!shareBtn) return;
    btn = document.createElement("button");
    btn.className = "print-doc-btn";
    btn.id = "print-doc-btn";
    btn.title = "Save as PDF";
    btn.innerHTML = "&#x1F4C4; Save as PDF";
    shareBtn.parentNode.insertBefore(btn, shareBtn);
  }

  btn.addEventListener("click", () => {
    enterPrintMode();
  });
}

function enterPrintMode() {
  document.body.classList.add("print-mode");

  // Collect all h2 headings as sections
  const headings = Array.from(document.querySelectorAll(".content h2"));

  // Build section wrappers: each section = from h2 to next h2 (or end)
  // We mark each element with data-print-section index
  const sections = [];
  const allContent = Array.from(document.querySelector(".content").childNodes);

  if (headings.length > 0) {
    let currentIdx = -1; // -1 = preamble (before first h2)
    for (const node of document.querySelector(".content").children) {
      if (node.tagName === "H2") {
        currentIdx++;
        sections[currentIdx] = sections[currentIdx] || { heading: node, elements: [] };
        sections[currentIdx].heading = node;
        node.dataset.printSection = currentIdx;
      } else if (currentIdx >= 0) {
        sections[currentIdx] = sections[currentIdx] || { heading: null, elements: [] };
        sections[currentIdx].elements = sections[currentIdx].elements || [];
        sections[currentIdx].elements.push(node);
        node.dataset.printSection = currentIdx;
      }
    }
  }

  // Build the picker panel
  const picker = document.createElement("div");
  picker.id = "print-section-picker";

  const titleEl = document.createElement("div");
  titleEl.className = "psp-title";
  titleEl.textContent = "Sections to include";
  picker.appendChild(titleEl);

  if (sections.length === 0) {
    // No headings — just print the whole doc
    const hint = document.createElement("div");
    hint.className = "psp-hint";
    hint.textContent = "No sections detected — full doc will print.";
    picker.appendChild(hint);
  } else {
    const list = document.createElement("div");
    list.className = "psp-section-list";

    sections.forEach((sec, i) => {
      const label = sec.heading ? sec.heading.textContent.trim() : "(intro)";
      const item = document.createElement("div");
      item.className = "psp-section-item";

      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.checked = true;
      cb.id = `psp-cb-${i}`;
      cb.addEventListener("change", () => {
        const hidden = !cb.checked;
        if (sec.heading) sec.heading.classList.toggle("print-section-hidden", hidden);
        if (sec.elements) sec.elements.forEach(el => el.classList.toggle("print-section-hidden", hidden));
      });

      const lbl = document.createElement("label");
      lbl.htmlFor = `psp-cb-${i}`;
      lbl.textContent = label;

      item.appendChild(cb);
      item.appendChild(lbl);
      list.appendChild(item);
    });
    picker.appendChild(list);
  }

  const hint = document.createElement("div");
  hint.className = "psp-hint";
  hint.innerHTML = "Click <b>Save as PDF</b> below &rarr; in the dialog, choose <b>Save as PDF</b> as the destination";
  picker.appendChild(hint);

  const actions = document.createElement("div");
  actions.className = "psp-actions";

  const printBtn = document.createElement("button");
  printBtn.className = "psp-btn psp-btn-print";
  printBtn.textContent = "📄 Save as PDF";
  printBtn.addEventListener("click", () => {
    window.print();
  });

  const cancelBtn = document.createElement("button");
  cancelBtn.className = "psp-btn psp-btn-cancel";
  cancelBtn.textContent = "Cancel";
  cancelBtn.addEventListener("click", () => {
    exitPrintMode(picker, sections);
  });

  actions.appendChild(cancelBtn);
  actions.appendChild(printBtn);
  picker.appendChild(actions);

  document.body.appendChild(picker);

  // After print dialog closes, exit print mode automatically
  window.addEventListener("afterprint", () => {
    exitPrintMode(picker, sections);
  }, { once: true });
}

function exitPrintMode(picker, sections) {
  document.body.classList.remove("print-mode");
  if (picker && picker.parentNode) picker.remove();
  // Un-hide all sections
  if (sections) {
    sections.forEach(sec => {
      if (sec.heading) sec.heading.classList.remove("print-section-hidden");
      if (sec.elements) sec.elements.forEach(el => el.classList.remove("print-section-hidden"));
    });
  }
}

// ── init ───────────────────────────────────────────────────────────────────
// ── Session exclude / delete buttons (session-history pages) ─────────────

function showSessionConfirmDialog(short, onConfirm) {
  // Remove any existing dialog
  const existing = document.getElementById("session-confirm-dialog");
  if (existing) existing.remove();

  const overlay = document.createElement("div");
  overlay.id = "session-confirm-dialog";
  overlay.innerHTML = `
    <div class="scd-box">
      <div class="scd-title">Remove session <code>${short}…</code> from history?</div>
      <div class="scd-body">The session will be hidden from this doc.</div>
      <label class="scd-delete-label">
        <input type="checkbox" id="scd-delete-check">
        Also permanently delete session file from disk
      </label>
      <div class="scd-warn" id="scd-warn" style="display:none">⚠ This cannot be undone — the session transcript will be deleted.</div>
      <div class="scd-actions">
        <button class="scd-cancel">Cancel</button>
        <button class="scd-confirm">Remove</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);

  const box        = overlay.querySelector(".scd-box");
  const check      = overlay.querySelector("#scd-delete-check");
  const warn       = overlay.querySelector("#scd-warn");
  const confirmBtn = overlay.querySelector(".scd-confirm");
  const cancelBtn  = overlay.querySelector(".scd-cancel");

  check.addEventListener("change", () => {
    warn.style.display = check.checked ? "" : "none";
    confirmBtn.textContent = check.checked ? "Remove & Delete" : "Remove";
    confirmBtn.classList.toggle("scd-confirm-danger", check.checked);
  });

  const close = () => overlay.remove();
  cancelBtn.addEventListener("click", close);
  overlay.addEventListener("click", (e) => { if (e.target === overlay) close(); });

  confirmBtn.addEventListener("click", () => {
    close();
    onConfirm(check.checked);
  });
}

async function handleExcludeSession(btn) {
  const folder     = btn.dataset.folder;
  const session_id = btn.dataset.sessionId;
  const short      = session_id.slice(0, 8);

  showSessionConfirmDialog(short, async (isDelete) => {
    btn.disabled = true;
    btn.textContent = "⏳";
    const endpoint = isDelete ? "/delete-session" : "/exclude-session";
    try {
      const r = await fetch(API + endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ folder, session_id }),
      });
      const d = await r.json();
      if (r.ok) {
        const row = btn.closest(".session-row");
        if (row) { row.style.opacity = "0.4"; row.style.pointerEvents = "none"; }
        btn.textContent = "✓";
      } else {
        alert((isDelete ? "Delete" : "Exclude") + " failed: " + (d.error || "unknown error"));
        btn.disabled = false;
        btn.textContent = "✕";
      }
    } catch(e) {
      alert((isDelete ? "Delete" : "Exclude") + " failed: " + e.message);
      btn.disabled = false;
      btn.textContent = "✕";
    }
  });
}

document.addEventListener("DOMContentLoaded", () => {
  if (window.REMOTE_USER && window.REMOTE_USER !== "anonymous") {
    const badge = document.createElement("span");
    badge.className = "remote-user-badge";
    badge.textContent = window.REMOTE_USER;
    const header = document.querySelector(".site-header");
    const firstBtn = header && header.querySelector("button");
    if (firstBtn) header.insertBefore(badge, firstBtn);
    else if (header) header.appendChild(badge);
  }

  // Guest mode: IS_OWNER is injected by the server. When false, hide all write
  // controls and show a read-only banner.
  if (window.IS_OWNER === false) {
    ["edit-doc-btn", "share-btn", "sel-chip", "comment-submit-btn"].forEach(id => {
      const el = document.getElementById(id);
      if (el) el.style.display = "none";
    });
    const form = document.querySelector(".comment-form");
    if (form) form.style.display = "none";
    const header = document.querySelector(".site-header");
    if (header) {
      const banner = document.createElement("span");
      banner.className = "guest-read-only-badge";
      banner.textContent = "Read-only access";
      header.appendChild(banner);
    }
  }

  initScrollspy();
  initCopyButtons();
  initMermaid();
  loadComments();
  initPrintMode();

  // wire submit button (also works if onclick= attr is used in template)
  const submitBtn = document.getElementById("comment-submit-btn");
  if (submitBtn) submitBtn.addEventListener("click", submitComment);

  // wire session exclude/delete buttons (session-history pages)
  document.querySelectorAll(".session-exclude-btn").forEach(btn => {
    btn.addEventListener("click", (e) => { e.preventDefault(); e.stopPropagation(); handleExcludeSession(btn); });
  });
});
