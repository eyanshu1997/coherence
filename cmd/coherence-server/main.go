package main

import (
	"coherence/internal/config"
	"coherence/internal/docgen"
	"coherence/internal/server"
	"coherence/internal/versioning"
	"fmt"
	"net/http"
	"os"
	"time"
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

	if cfg.VersioningEnabled {
		store, err := versioning.New(cfg.VersionsDir, cfg.DataDir,
			time.Duration(cfg.VersionDebounceSec)*time.Second)
		if err != nil {
			// Not fatal: serving docs matters more than keeping history.
			fmt.Fprintf(os.Stderr, "WARNING: version history disabled: %v\n", err)
		} else {
			h.SetVersionStore(store)
			fmt.Fprintf(os.Stdout, "Version history: %s (debounce %ds)\n", store.GitDir(), cfg.VersionDebounceSec)
			// Capture anything that changed while the server was down.
			go func() {
				if err := store.CommitNow("server start"); err != nil {
					fmt.Fprintf(os.Stderr, "WARNING: startup snapshot failed: %v\n", err)
				}
			}()
			if cfg.VersionSweepSec > 0 {
				go store.RunPeriodic(time.Duration(cfg.VersionSweepSec)*time.Second, nil)
				fmt.Fprintf(os.Stdout, "Version sweep: every %ds\n", cfg.VersionSweepSec)
			}
		}
	}

	addr := fmt.Sprintf("%s:%s", cfg.CoherenceBind, cfg.CoherencePort)
	fmt.Fprintf(os.Stdout, "Docs server listening on %s\n", addr)
	if err := http.ListenAndServe(addr, h); err != nil {
		fmt.Fprintln(os.Stderr, "Server error:", err)
		os.Exit(1)
	}
}
