package docgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
)

type Config struct {
	DataDir     string
	DocBase     string
	JiraBaseURL string
	GitHubOrg   string
	FooterText  string
}

type docData struct {
	Title       string
	GeneratedAt string
	Body        string
	FolderJSON  string
	FileJSON    string
	TitleJSON   string
	RawMarkdown string
	FooterText  string
	AssetVer    string
	LogoSVG     string
}

type homeData struct {
	Count       int
	Plural      string
	UpdatedAt   string
	FolderCards string
	FooterText  string
	AssetVer    string
	LogoSVG     string
}

type folderData struct {
	FolderName  string
	FolderPath  string
	ParentCrumb string
	Count       int
	Plural      string
	DocCards    string
	FooterText  string
	AssetVer    string
	LogoSVG     string
}

var (
	docTmpl    = template.Must(template.New("doc").Parse(docTemplate))
	homeTmpl   = template.Must(template.New("home").Parse(homeTemplate))
	folderTmpl = template.Must(template.New("folder").Parse(folderTemplate))
)

// GenerateDoc creates a styled HTML doc at dataDir/folder/filename.html
// and regenerates all parent indexes. Returns the URL.
func GenerateDoc(cfg *Config, folder, title, content, filename string) (string, error) {
	folderPath := filepath.Join(cfg.DataDir, filepath.FromSlash(folder))
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		return "", err
	}
	os.Chmod(folderPath, 0755)

	if filename == "" {
		slug := slugify(title)
		filename = slug + ".html"
	}
	if !strings.HasSuffix(filename, ".html") {
		filename += ".html"
	}

	body := MarkdownToHTML(content, cfg.JiraBaseURL, cfg.GitHubOrg)
	now := time.Now().Format("Jan 02, 2006 15:04")
	docName := strings.TrimSuffix(filename, ".html")

	rawEscaped := strings.ReplaceAll(content, "</", "<\\/")

	fj, _ := json.Marshal(folder)
	filej, _ := json.Marshal(docName)
	titlej, _ := json.Marshal(title)

	data := docData{
		Title:       html.EscapeString(title),
		GeneratedAt: now,
		Body:        body,
		FolderJSON:  string(fj),
		FileJSON:    string(filej),
		TitleJSON:   string(titlej),
		RawMarkdown: rawEscaped,
		FooterText:  cfg.FooterText,
		AssetVer:    AssetVer,
		LogoSVG:     logoSVG,
	}

	var buf bytes.Buffer
	if err := docTmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	outPath := filepath.Join(folderPath, filename)
	if err := os.WriteFile(outPath, buf.Bytes(), 0644); err != nil {
		return "", err
	}
	os.Chmod(outPath, 0644)

	// regenerate all ancestor indexes
	p := folderPath
	for p != cfg.DataDir && p != filepath.Dir(p) {
		os.Chmod(p, 0755)
		regenerateFolderIndex(cfg, p)
		p = filepath.Dir(p)
	}
	regenerateHomeIndex(cfg)

	url := fmt.Sprintf("%s/%s/%s", cfg.DocBase, folder, filename)
	fmt.Printf("Document written: %s\nURL: %s\n", outPath, url)
	return url, nil
}

// ReindexAll rebuilds all index.html files, deepest-first.
func ReindexAll(cfg *Config) {
	var dirs []string
	filepath.Walk(cfg.DataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || strings.HasPrefix(info.Name(), ".") {
			return nil
		}
		dirs = append(dirs, path)
		return nil
	})
	// sort deepest first
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, d := range dirs {
		if d == cfg.DataDir {
			continue
		}
		os.Chmod(d, 0755)
		regenerateFolderIndex(cfg, d)
	}
	regenerateHomeIndex(cfg)
}

var viewableExts = map[string]bool{".html": true, ".log": true, ".md": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true}

func regenerateFolderIndex(cfg *Config, folderPath string) {
	relFolder, _ := filepath.Rel(cfg.DataDir, folderPath)
	relFolder = filepath.ToSlash(relFolder)

	// breadcrumb from ancestors
	var ancestors []string
	p := filepath.Dir(folderPath)
	for p != cfg.DataDir && p != filepath.Dir(p) {
		rel, _ := filepath.Rel(cfg.DataDir, p)
		ancestors = append([]string{filepath.ToSlash(rel)}, ancestors...)
		p = filepath.Dir(p)
	}
	var crumbParts []string
	for _, a := range ancestors {
		name := filepath.Base(a)
		crumbParts = append(crumbParts, fmt.Sprintf(`<a href="/%s/" class="bc-link">%s</a> <span class="bc-sep">›</span> `, a, name))
	}
	parentCrumb := strings.Join(crumbParts, "")

	subdirs := sortedDirs(folderPath)
	docs := sortedDocs(folderPath)

	var cards []string

	// subfolder cards
	for _, sub := range subdirs {
		subSlug := relFolder + "/" + sub.Name()
		subDocs := sortedDocs(sub.Path)
		subSubdirs := sortedDirs(sub.Path)
		childCount := len(subDocs) + len(subSubdirs)
		noun := "items"
		if childCount == 1 {
			noun = "item"
		}
		latestTs, latestStr := latestMtime(sub.Path, subDocs, subSubdirs)
		preview := buildPreviewLines(subSubdirs, subDocs, 4)
		cards = append(cards, fmt.Sprintf(
			"    <div class=\"folder-card-wrap\">\n"+
				"      <a href=\"/%s/%s/\" class=\"folder-card\" data-name=\"%s\" data-mtime=\"%d\">\n"+
				"        <div class=\"folder-card-head\">\n"+
				"          <span class=\"folder-name\">%s</span>\n"+
				"          <span class=\"folder-count\">%d %s</span>\n"+
				"        </div>\n"+
				"        <div class=\"folder-latest\">%s</div>\n"+
				"        <div class=\"folder-docs-preview\">\n%s\n        </div>\n"+
				"      </a>\n"+
				"      <button class=\"folder-rename-btn\" data-folder=\"%s\" title=\"Rename this folder\">&#x270E;</button>\n"+
				"      <button class=\"folder-move-btn\" data-folder=\"%s\" title=\"Move this folder\">&#x2197;</button>\n"+
				"      <button class=\"folder-delete-btn\" data-folder=\"%s\" title=\"Delete this folder\">&#x1F5D1;</button>\n"+
				"    </div>",
			relFolder, sub.Name(), sub.Name(), latestTs*1000,
			sub.Name(), childCount, noun, latestStr,
			preview,
			subSlug, subSlug, subSlug,
		))
	}

	// doc cards
	for _, doc := range docs {
		mtime := time.Unix(doc.Mtime, 0).Format("Jan 02, 2006 15:04")
		title := docTitle(doc.Path, doc.Name())
		cards = append(cards, fmt.Sprintf(
			"    <div class=\"doc-card-wrap\">\n"+
				"      <a href=\"/%s/%s\" class=\"doc-card\" data-name=\"%s\" data-mtime=\"%d\">\n"+
				"        <div class=\"doc-card-icon\">&#x1F4C4;</div>\n"+
				"        <div class=\"doc-card-body\">\n"+
				"          <div class=\"doc-card-title\">%s</div>\n"+
				"          <div class=\"doc-card-meta\">\n"+
				"            <span class=\"doc-card-file\">%s</span>\n"+
				"            <span>%s</span>\n"+
				"          </div>\n"+
				"        </div>\n"+
				"        <span class=\"doc-card-arrow\">›</span>\n"+
				"      </a>\n"+
				"      <button class=\"doc-rename-btn\" data-folder=\"%s\" data-file=\"%s\" title=\"Rename this document\">&#x270E;</button>\n"+
				"      <button class=\"doc-move-btn\" data-folder=\"%s\" data-file=\"%s\" title=\"Move this document\">&#x2197;</button>\n"+
				"      <button class=\"doc-delete-btn\" data-folder=\"%s\" data-file=\"%s\" title=\"Delete this document\">&#x1F5D1;</button>\n"+
				"    </div>",
			relFolder, doc.Name(), strings.TrimSuffix(doc.Name(), ".html"), doc.Mtime*1000,
			title, doc.Name(), mtime,
			relFolder, strings.TrimSuffix(doc.Name(), ".html"),
			relFolder, strings.TrimSuffix(doc.Name(), ".html"),
			relFolder, strings.TrimSuffix(doc.Name(), ".html"),
		))
	}

	cardsHTML := strings.Join(cards, "\n")
	if cardsHTML == "" {
		cardsHTML = "    <div class=\"empty-state\">No documents in this folder yet.</div>"
	}
	count := len(subdirs) + len(docs)
	plural := "s"
	if count == 1 {
		plural = ""
	}

	var buf bytes.Buffer
	folderTmpl.Execute(&buf, folderData{
		FolderName:  filepath.Base(folderPath),
		FolderPath:  relFolder,
		ParentCrumb: parentCrumb,
		Count:       count,
		Plural:      plural,
		DocCards:    cardsHTML,
		FooterText:  cfg.FooterText,
		AssetVer:    AssetVer,
		LogoSVG:     logoSVG,
	})
	idx := filepath.Join(folderPath, "index.html")
	os.WriteFile(idx, buf.Bytes(), 0644)
	os.Chmod(idx, 0644)
	fmt.Printf("Folder index updated: %s\n", idx)
}

func regenerateHomeIndex(cfg *Config) {
	entries := sortedDirs(cfg.DataDir)

	var cards []string
	for _, entry := range entries {
		docs := sortedDocs(entry.Path)
		subdirs := sortedDirs(entry.Path)
		childCount := len(docs) + len(subdirs)
		noun := "items"
		if childCount == 1 {
			noun = "item"
		}
		latestTs, latestStr := latestMtime(entry.Path, docs, subdirs)
		preview := buildPreviewLines(subdirs, docs, 4)
		cards = append(cards, fmt.Sprintf(
			"    <div class=\"folder-card-wrap\">\n"+
				"      <a href=\"/%s/\" class=\"folder-card\" data-name=\"%s\" data-mtime=\"%d\">\n"+
				"        <div class=\"folder-card-head\">\n"+
				"          <span class=\"folder-name\">%s</span>\n"+
				"          <span class=\"folder-count\">%d %s</span>\n"+
				"        </div>\n"+
				"        <div class=\"folder-latest\">%s</div>\n"+
				"        <div class=\"folder-docs-preview\">\n%s\n        </div>\n"+
				"      </a>\n"+
				"      <button class=\"folder-rename-btn\" data-folder=\"%s\" title=\"Rename this folder\">&#x270E;</button>\n"+
				"      <button class=\"folder-move-btn\" data-folder=\"%s\" title=\"Move this folder\">&#x2197;</button>\n"+
				"      <button class=\"folder-delete-btn\" data-folder=\"%s\" title=\"Delete this folder\">&#x1F5D1;</button>\n"+
				"    </div>",
			entry.Name(), entry.Name(), latestTs*1000,
			entry.Name(), childCount, noun, latestStr,
			preview,
			entry.Name(), entry.Name(), entry.Name(),
		))
	}

	cardsHTML := strings.Join(cards, "\n")
	if cardsHTML == "" {
		cardsHTML = "    <div class=\"empty-state\">No documents yet. Use /generate-doc in a Claude session to create one.</div>"
	}
	plural := "s"
	if len(entries) == 1 {
		plural = ""
	}

	var buf bytes.Buffer
	homeTmpl.Execute(&buf, homeData{
		Count:       len(entries),
		Plural:      plural,
		UpdatedAt:   time.Now().Format("Jan 02, 2006 15:04"),
		FolderCards: cardsHTML,
		FooterText:  cfg.FooterText,
		AssetVer:    AssetVer,
		LogoSVG:     logoSVG,
	})
	idx := filepath.Join(cfg.DataDir, "index.html")
	os.WriteFile(idx, buf.Bytes(), 0644)
	os.Chmod(idx, 0644)
	fmt.Printf("Home index updated: %s\n", idx)
}

// helpers

type dirEntry struct {
	Path  string
	Mtime int64
	name  string
}

func (d dirEntry) Name() string { return d.name }

type docEntry struct {
	Path  string
	Mtime int64
	name  string
}

func (d docEntry) Name() string { return d.name }

func sortedDirs(path string) []dirEntry {
	entries, _ := os.ReadDir(path)
	var dirs []dirEntry
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, _ := e.Info()
		dirs = append(dirs, dirEntry{Path: filepath.Join(path, e.Name()), Mtime: info.ModTime().Unix(), name: e.Name()})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Mtime > dirs[j].Mtime })
	return dirs
}

func sortedDocs(path string) []docEntry {
	entries, _ := os.ReadDir(path)
	var docs []docEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if !viewableExts[ext] {
			continue
		}
		if e.Name() == "index.html" || strings.HasSuffix(e.Name(), ".comments.json") {
			continue
		}
		info, _ := e.Info()
		docs = append(docs, docEntry{Path: filepath.Join(path, e.Name()), Mtime: info.ModTime().Unix(), name: e.Name()})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Mtime > docs[j].Mtime })
	return docs
}

func latestMtime(folderPath string, docs []docEntry, subdirs []dirEntry) (int64, string) {
	var latest int64
	info, err := os.Stat(folderPath)
	if err == nil {
		latest = info.ModTime().Unix()
	}
	for _, d := range docs {
		if d.Mtime > latest {
			latest = d.Mtime
		}
	}
	for _, s := range subdirs {
		if s.Mtime > latest {
			latest = s.Mtime
		}
	}
	if latest == 0 {
		return 0, "empty"
	}
	return latest, time.Unix(latest, 0).Format("Jan 02, 2006 15:04")
}

func buildPreviewLines(subdirs []dirEntry, docs []docEntry, max int) string {
	var lines []string
	for _, s := range subdirs {
		if len(lines) >= max {
			break
		}
		lines = append(lines, fmt.Sprintf("      <div class=\"folder-doc-hint\">%s</div>", s.Name()))
	}
	for _, d := range docs {
		if len(lines) >= max {
			break
		}
		title := docTitle(d.Path, d.Name())
		lines = append(lines, fmt.Sprintf("      <div class=\"folder-doc-hint\">%s</div>", title))
	}
	if len(lines) == 0 {
		return "      <div class=\"folder-doc-hint\" style=\"font-style:italic;color:var(--text3)\">No documents yet</div>"
	}
	return strings.Join(lines, "\n")
}

var h1Re = regexp.MustCompile(`<h1[^>]*>([^<]+)</h1>`)

func docTitle(path, name string) string {
	data, err := os.ReadFile(path)
	if err == nil {
		if m := h1Re.FindSubmatch(data); m != nil {
			t := strings.TrimSpace(string(m[1]))
			if len(t) > 70 {
				t = t[:70]
			}
			return t
		}
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.Title(base)
}

func slugify(title string) string {
	slug := strings.ToLower(title)
	slug = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(slug, "-")
	slug = regexp.MustCompile(`-+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 50 {
		slug = slug[:50]
	}
	return slug
}
