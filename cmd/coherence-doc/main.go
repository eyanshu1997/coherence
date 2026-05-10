package main

import (
	"coherence/internal/auth"
	"coherence/internal/config"
	"coherence/internal/docgen"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cfg := config.Load()
	dgCfg := &docgen.Config{
		DataDir:     cfg.DataDir,
		DocBase:     cfg.DocBase,
		JiraBaseURL: cfg.JiraBaseURL,
		GitHubOrg:   cfg.GitHubOrg,
		FooterText:  cfg.FooterText,
	}

	switch os.Args[1] {
	case "generate":
		cmdGenerate(dgCfg, os.Args[2:])
	case "reindex":
		docgen.ReindexAll(dgCfg)
	case "set-password":
		cmdSetPassword(cfg)
	case "share":
		cmdShare(cfg, os.Args[2:])
	default:
		// Legacy flag-based mode for backward compat with generate_doc.py CLI
		cmdLegacy(dgCfg, cfg)
	}
}

func cmdGenerate(cfg *docgen.Config, args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	folder := fs.String("folder", "", "Folder path (required)")
	title := fs.String("title", "", "Document title (required)")
	content := fs.String("content", "", "Markdown content")
	contentFile := fs.String("content-file", "", "Read content from file")
	filename := fs.String("filename", "", "Output filename")
	fs.Parse(args)

	body := *content
	if *contentFile != "" {
		data, err := os.ReadFile(*contentFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error reading content file:", err)
			os.Exit(1)
		}
		body = string(data)
	}
	if *folder == "" || *title == "" || body == "" {
		fmt.Fprintln(os.Stderr, "Error: --folder, --title, and content are required")
		os.Exit(1)
	}
	url, err := docgen.GenerateDoc(cfg, *folder, *title, body, *filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	fmt.Println(url)
}

func cmdSetPassword(cfg *config.Config) {
	fmt.Print("New password: ")
	var pw string
	fmt.Scanln(&pw)
	if pw == "" {
		fmt.Fprintln(os.Stderr, "Error: password cannot be empty")
		os.Exit(1)
	}

	existing := auth.LoadConfig(cfg.AuthFile)
	var secret string
	if existing != nil && existing.SessionSecret != "" {
		secret = existing.SessionSecret
	} else {
		b := make([]byte, 20)
		rand.Read(b)
		secret = fmt.Sprintf("%x", b)
	}

	authCfg := &auth.AuthConfig{
		PasswordHash:  auth.HashPassword(pw),
		SessionSecret: secret,
	}
	if err := auth.SaveConfig(cfg.AuthFile, authCfg); err != nil {
		fmt.Fprintln(os.Stderr, "Error saving config:", err)
		os.Exit(1)
	}
	fmt.Printf("Password set. Config written to %s\n", cfg.AuthFile)
	fmt.Println("Restart the server: sudo systemctl restart coherence-server")
}

func cmdShare(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("share", flag.ExitOnError)
	days := fs.Int("days", 30, "Token validity in days")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: coherence-doc share [--days N] /path/to/doc.html")
		os.Exit(1)
	}
	path := fs.Arg(0)
	token, expires, err := auth.CreateShare(cfg.SharesFile, path, *days)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	expStr := time.Unix(expires, 0).Format("Jan 02, 2006")
	url := cfg.DocBase + path + "?share=" + token
	fmt.Printf("Share URL (expires %s):\n%s\n", expStr, url)
	out := map[string]any{"token": token, "url": url, "expires": expStr, "path": path}
	enc, _ := json.Marshal(out)
	fmt.Println(string(enc))
}

// cmdLegacy handles the old generate_doc.py --folder/--title/--content/--reindex flags
func cmdLegacy(dgCfg *docgen.Config, cfg *config.Config) {
	fs := flag.NewFlagSet("coherence-doc", flag.ExitOnError)
	folder := fs.String("folder", "", "Folder")
	title := fs.String("title", "", "Title")
	content := fs.String("content", "", "Content")
	contentFile := fs.String("content-file", "", "Content file")
	filename := fs.String("filename", "", "Filename")
	reindex := fs.Bool("reindex", false, "Reindex")
	setPassword := fs.String("set-password", "", "Set password")
	sharePath := fs.String("share", "", "Create share token for path")
	days := fs.Int("days", 30, "Share token days")
	fs.Parse(os.Args[1:])

	if *setPassword != "" {
		b := make([]byte, 20)
		rand.Read(b)
		secret := fmt.Sprintf("%x", b)
		existing := auth.LoadConfig(cfg.AuthFile)
		if existing != nil && existing.SessionSecret != "" {
			secret = existing.SessionSecret
		}
		authCfg := &auth.AuthConfig{
			PasswordHash:  auth.HashPassword(*setPassword),
			SessionSecret: secret,
		}
		auth.SaveConfig(cfg.AuthFile, authCfg)
		fmt.Printf("Password set. Config written to %s\n", cfg.AuthFile)
		return
	}

	if *sharePath != "" {
		token, expires, err := auth.CreateShare(cfg.SharesFile, *sharePath, *days)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		expStr := time.Unix(expires, 0).Format("Jan 02, 2006")
		url := cfg.DocBase + *sharePath + "?share=" + token
		fmt.Printf("Share URL (expires %s):\n%s\n", expStr, url)
		return
	}

	if *reindex {
		docgen.ReindexAll(dgCfg)
		return
	}

	body := *content
	if *contentFile != "" {
		data, _ := os.ReadFile(*contentFile)
		body = string(data)
	}
	if *folder == "" || *title == "" || body == "" {
		fmt.Fprintln(os.Stderr, "Error: --folder, --title, and --content (or --content-file) are required")
		os.Exit(1)
	}
	url, err := docgen.GenerateDoc(dgCfg, *folder, *title, body, *filename)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, _ := json.Marshal(map[string]string{"url": url})
	fmt.Println(string(out))
}

func usage() {
	fmt.Println(`coherence-doc — document generator

Commands:
  generate --folder F --title T [--content C] [--content-file F] [--filename F]
  reindex
  set-password
  share [--days N] /path/to/doc.html

Legacy flags (generate_doc.py compat):
  --folder --title --content --content-file --filename --reindex --set-password --share --days`)
}
