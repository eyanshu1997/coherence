package auth

import (
	"encoding/json"
	"os"
)

type AuthConfig struct {
	PasswordHash  string `json:"password_hash"`
	SessionSecret string `json:"session_secret"`
}

func LoadConfig(path string) *AuthConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg AuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	if cfg.PasswordHash == "" || cfg.SessionSecret == "" {
		return nil
	}
	return &cfg
}

func SaveConfig(path string, cfg *AuthConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}
