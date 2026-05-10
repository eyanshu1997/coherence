package main

import (
	"coherence/internal/config"
	"coherence/internal/docgen"
	"coherence/internal/server"
	"fmt"
	"net/http"
	"os"
)

func main() {
	cfg := config.Load()
	dgCfg := &docgen.Config{
		DataDir:     cfg.DataDir,
		DocBase:     cfg.DocBase,
		JiraBaseURL: cfg.JiraBaseURL,
		GitHubOrg:   cfg.GitHubOrg,
		FooterText:  cfg.FooterText,
	}

	h := server.New(cfg, dgCfg)
	addr := fmt.Sprintf("%s:%s", cfg.CoherenceBind, cfg.CoherencePort)
	fmt.Fprintf(os.Stdout, "Docs server listening on %s\n", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		fmt.Fprintln(os.Stderr, "Server error:", err)
		os.Exit(1)
	}
}
