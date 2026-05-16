package docgen

// AssetVer is injected at build time via -ldflags "-X coherence/internal/docgen.AssetVer=vNN"
var AssetVer = "v29"

const logoSVG = `<svg width="22" height="22" viewBox="0 0 22 22" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><rect x="4" y="2" width="11" height="14" rx="2" fill="#0969da" opacity="0.15"/><rect x="4" y="2" width="11" height="14" rx="2" stroke="#0969da" stroke-width="1.5"/><rect x="7" y="6" width="14" height="14" rx="2" fill="#ffffff" stroke="#0969da" stroke-width="1.5"/><path d="M13 10.5l-2.5 3.5h2l-1 3.5 3.5-4.5h-2.2l1.2-2.5z" fill="#0969da"/></svg>`

const docTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.Title}} — coherence</title>
<link rel="stylesheet" href="/assets/theme.css?{{.AssetVer}}">
</head>
<body>
<header class="site-header">
  <a class="site-logo" href="/">
    {{.LogoSVG}}
    <span class="logo-sep">/</span>
    <span class="logo-name">coherence</span>
  </a>
  <div class="header-breadcrumb" id="header-breadcrumb">
    <span class="bc-sep">›</span>
    <a href="#" id="bc-folder-link" class="bc-folder"></a>
    <span class="bc-sep">›</span>
    <span class="bc-current" id="bc-doc"></span>
  </div>
  <span class="header-tag" id="header-tag"></span>
  <button class="edit-doc-btn" id="edit-doc-btn" title="Edit this document">&#x270E; Edit</button>
  <button class="print-doc-btn" id="print-doc-btn" title="Save as PDF">&#x1F4C4; Save as PDF</button>
  <button class="share-btn" id="share-btn" title="Get shareable link">&#x1F517; Share</button>
  <div id="share-popover" class="share-popover" style="display:none">
    <div class="share-popover-title">Share this page</div>
    <div class="share-row">
      <label class="share-label">Expires in</label>
      <select id="share-days" class="share-select">
        <option value="7">7 days</option>
        <option value="30" selected>30 days</option>
        <option value="90">90 days</option>
        <option value="365">1 year</option>
      </select>
    </div>
    <button class="share-generate-btn" id="share-generate-btn">Generate link</button>
    <div id="share-result" class="share-result" style="display:none">
      <input id="share-url-input" class="share-url-input" type="text" readonly>
      <button class="share-copy-btn" id="share-copy-btn">Copy</button>
      <div class="share-expires" id="share-expires"></div>
    </div>
    <div class="share-status" id="share-status"></div>
  </div>
</header>
<div class="page">
  <nav class="sidebar" id="doc-sidebar">
    <div class="sidebar-section">
      <span class="sidebar-label">In this doc</span>
      <div id="sidebar-nav"></div>
    </div>
  </nav>
  <main class="main">
    <div class="doc-header">
      <h1>{{.Title}}</h1>
      <div class="doc-meta">
        <span>{{.GeneratedAt}}</span>
      </div>
    </div>
    <div class="content">
{{.Body}}
    </div>

    <button id="sel-chip">&#x1F4AC; Add comment</button>
    <div id="sel-popover">
      <div class="pop-quote-box" id="pop-quote"></div>
      <textarea id="pop-input" placeholder="Your note for Claude&#x2026;" rows="3"></textarea>
      <div class="pop-actions">
        <button class="pop-save" id="pop-save-btn">Save</button>
        <button class="pop-cancel" id="pop-cancel-btn">Cancel</button>
        <span class="pop-hint" id="pop-status">&#x2318;&#x21B5; to save</span>
      </div>
    </div>

    <div class="comments-section">
      <div class="comments-header">
        <span class="comments-title">Comments &amp; Feedback</span>
        <span class="comments-hint">Select text to add inline &mdash; or type below. Read by Claude via <code id="comments-load-cmd">/load-doc</code><script>document.getElementById("comments-load-cmd").textContent="/load-doc "+(window.DOC_FOLDER||"");</script></span>
      </div>
      <div class="comment-list" id="comment-list"></div>
      <div class="comment-form">
        <textarea id="comment-input" placeholder="Add a note or instruction for Claude&#x2026;"></textarea>
        <div class="comment-form-footer">
          <button class="comment-submit" id="comment-submit-btn">Add Comment</button>
          <span class="comment-save-status" id="comment-status"></span>
        </div>
      </div>
    </div>

    <div class="doc-footer">
      <div class="doc-footer-left">
        <a href="/">coherence</a> &middot; {{.GeneratedAt}}
      </div>
      <div class="doc-footer-right">
        {{.FooterText}}
      </div>
    </div>
  </main>
</div>
<script>window.DOC_FOLDER = {{.FolderJSON}}; window.DOC_FILE = {{.FileJSON}}; window.DOC_TITLE = {{.TitleJSON}};
(function() {
  var folder = window.DOC_FOLDER || "";
  var file   = window.DOC_FILE   || "";
  var folderDisplay = folder.split("/").filter(Boolean).pop() || folder;
  var link = document.getElementById("bc-folder-link");
  if (link) { link.textContent = folderDisplay; link.href = "/" + folder + "/"; }
  var doc  = document.getElementById("bc-doc");
  if (doc)  { doc.textContent  = file; }
  var tag  = document.getElementById("header-tag");
  if (tag)  { tag.textContent  = folderDisplay; }
})();
</script>
<script type="application/x-markdown" id="doc-raw-markdown">{{.RawMarkdown}}</script>
<script type="module">
import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';
mermaid.initialize({ startOnLoad: false, theme: 'neutral', securityLevel: 'loose' });
window._mermaid = mermaid;
</script>
<script src="/assets/shared.js?{{.AssetVer}}"></script>
<script src="/assets/doc.js?{{.AssetVer}}"></script>
</body>
</html>`

const homeTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>coherence</title>
<link rel="stylesheet" href="/assets/theme.css?{{.AssetVer}}">
<link rel="stylesheet" href="/assets/home.css?{{.AssetVer}}">
</head>
<body>
<div class="home-header">
  <div class="home-header-top">
    <a class="home-logo" href="/">
      {{.LogoSVG}}
      <span class="home-logo-sep">/</span>
      <span class="home-logo-name">coherence</span>
    </a>
    <div class="home-header-actions">
      <button class="new-folder-btn" id="new-folder-btn" title="Create a new folder">+ New Folder</button>
      <button class="new-doc-btn" id="new-doc-btn" title="Create a new document">+ New Doc</button>
    </div>
  </div>
  <p class="home-subtitle">Session documents, fix summaries, and issue notes.</p>
  <div class="home-stats">
    <span class="home-stat"><strong id="folder-count">{{.Count}} folder{{.Plural}}</strong></span>
    <span class="home-stat">&middot; Updated {{.UpdatedAt}}</span>
  </div>
</div>
<div class="home-main">
  <div class="search-bar-wrap">
    <input type="search" id="global-search" class="global-search-input" placeholder="Search docs… (/ to focus)" autocomplete="off" spellcheck="false">
    <span class="search-scope-hint" id="search-scope-hint"></span>
  </div>
  <div id="search-results" class="search-results" style="display:none"></div>
  <div class="section-label">Sessions &amp; Issues
    <span class="sort-controls">
      <button class="sort-btn active" data-sort="time" data-target="folder-grid">Recent</button>
      <button class="sort-btn" data-sort="alpha" data-target="folder-grid">A–Z</button>
    </span>
  </div>
  <div class="folder-grid" id="folder-grid">
{{.FolderCards}}
  </div>
  <div class="home-footer">
    <div class="home-footer-left">
      <a href="/">coherence</a> &middot; <a href="javascript:location.reload()">Refresh</a>
    </div>
    <div class="home-footer-right">
      {{.FooterText}}
    </div>
  </div>
</div>
<script src="/assets/shared.js?{{.AssetVer}}"></script>
<script src="/assets/home.js?{{.AssetVer}}"></script>
</body>
</html>`

const folderTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{.FolderName}} — coherence</title>
<link rel="stylesheet" href="/assets/theme.css?{{.AssetVer}}">
<link rel="stylesheet" href="/assets/home.css?{{.AssetVer}}">
</head>
<body>
<header class="folder-header">
  <a class="folder-header-logo" href="/">
    {{.LogoSVG}}
    <span class="folder-header-sep">/</span>
    <span class="folder-header-name">coherence</span>
  </a>
  <span class="folder-header-breadcrumb">{{.ParentCrumb}}<span class="bc-current">{{.FolderName}}</span></span>
  <span class="folder-header-tag">{{.FolderName}}</span>
  <button class="new-folder-btn" id="new-folder-btn" data-folder="{{.FolderPath}}" title="Create a new subfolder">+ New Folder</button>
  <button class="new-doc-btn" id="new-doc-btn" data-folder="{{.FolderPath}}" title="Create a new document">+ New Doc</button>
  <button class="upload-btn" id="upload-btn" data-folder="{{.FolderPath}}" title="Upload a file">&#x2191; Upload</button>
  <button class="share-btn" id="share-btn" title="Get shareable link">&#x1F517; Share folder</button>
  <div id="share-popover" class="share-popover" style="display:none">
    <div class="share-popover-title">Share this folder</div>
    <div class="share-row">
      <label class="share-label">Expires in</label>
      <select id="share-days" class="share-select">
        <option value="7">7 days</option>
        <option value="30" selected>30 days</option>
        <option value="90">90 days</option>
        <option value="365">1 year</option>
      </select>
    </div>
    <button class="share-generate-btn" id="share-generate-btn">Generate link</button>
    <div id="share-result" class="share-result" style="display:none">
      <input id="share-url-input" class="share-url-input" type="text" readonly>
      <button class="share-copy-btn" id="share-copy-btn">Copy</button>
      <div class="share-expires" id="share-expires"></div>
    </div>
    <div class="share-status" id="share-status"></div>
  </div>
</header>
<div class="folder-page-main">
  <div class="folder-page-title">{{.FolderName}}</div>
  <div class="folder-page-subtitle"><span id="folder-count">{{.Count}} document{{.Plural}}</span></div>
  <div class="search-bar-wrap">
    <input type="search" id="global-search" class="global-search-input" placeholder="Search docs… (/ to focus)" autocomplete="off" spellcheck="false">
    <span class="search-scope-hint" id="search-scope-hint"></span>
  </div>
  <div id="search-results" class="search-results" style="display:none"></div>
  <div class="section-label">Documents
    <span class="sort-controls">
      <button class="sort-btn active" data-sort="time" data-target="doc-list">Recent</button>
      <button class="sort-btn" data-sort="alpha" data-target="doc-list">A–Z</button>
    </span>
  </div>
  <div class="doc-list" id="doc-list">
{{.DocCards}}
  </div>
  <div class="home-footer">
    <div class="home-footer-left">
      <a href="/">coherence</a>
    </div>
    <div class="home-footer-right">
      {{.FooterText}}
    </div>
  </div>
</div>
<script>window.INDEX_FOLDER = "{{.FolderPath}}";</script>
<script src="/assets/shared.js?{{.AssetVer}}"></script>
</body>
</html>`
