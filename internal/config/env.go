package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DataDir      string
	DocHost      string
	DocPort      string
	DocScheme    string
	DocBase      string
	CoherencePort string
	CoherenceBind string
	JiraBaseURL  string
	GitHubOrg    string
	FooterText   string
	CompactThreshold string
	CoherenceHome string
	AuthFile     string
	SharesFile   string
}

func Load() *Config {
	home := os.Getenv("HOME")
	coherenceHome := os.Getenv("COHERENCE_HOME")
	if coherenceHome == "" {
		// fallback: directory of this binary
		exe, _ := os.Executable()
		coherenceHome = filepath.Dir(filepath.Dir(exe))
	}

	loadDotEnv(filepath.Join(coherenceHome, ".env"))

	dataDir := getenv("DOC_DATA_DIR", filepath.Join(home, ".coherence", "data"))
	docHost := getenv("DOC_HOST", "localhost")
	docPort := getenv("DOC_PORT", "8080")
	docScheme := getenv("DOC_SCHEME", defaultScheme(docHost, docPort))

	return &Config{
		DataDir:       dataDir,
		DocHost:       docHost,
		DocPort:       docPort,
		DocScheme:     docScheme,
		DocBase:       docScheme + "://" + docHost + ":" + docPort,
		CoherencePort: getenv("COHERENCE_PORT", "8080"),
		CoherenceBind: getenv("COHERENCE_BIND", "0.0.0.0"),
		JiraBaseURL:   strings.TrimRight(getenv("JIRA_BASE_URL", ""), "/"),
		GitHubOrg:     getenv("GITHUB_ORG", ""),
		FooterText:    getenv("FOOTER_TEXT", "Built with coherence"),
		CompactThreshold: getenv("COMPACT_THRESHOLD", "50"),
		CoherenceHome: coherenceHome,
		AuthFile:      filepath.Join(home, ".ssh", "doc-auth.json"),
		SharesFile:    filepath.Join(home, ".ssh", "doc-shares.json"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultScheme(host, port string) string {
	if host == "localhost" || host == "127.0.0.1" {
		return "http"
	}
	if port == "80" || port == "443" {
		return "https"
	}
	return "https"
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// strip inline comment
		if idx := strings.Index(line, " #"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if os.Getenv(k) == "" { // don't override existing env
			os.Setenv(k, v)
		}
	}
}
