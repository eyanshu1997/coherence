package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"time"
)

type shareEntry struct {
	Path    string `json:"path"`
	Expires int64  `json:"expires"`
	Created string `json:"created"`
}

func CreateShare(sharesFile, path string, days int) (string, int64, error) {
	shares := loadShares(sharesFile)
	b := make([]byte, 18)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	expires := time.Now().Unix() + int64(days)*86400
	shares[token] = shareEntry{
		Path:    path,
		Expires: expires,
		Created: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveShares(sharesFile, shares); err != nil {
		return "", 0, err
	}
	return token, expires, nil
}

func CheckShare(sharesFile, token, reqPath string) bool {
	shares := loadShares(sharesFile)
	entry, ok := shares[token]
	if !ok {
		return false
	}
	if time.Now().Unix() > entry.Expires {
		return false
	}
	sharePath := strings.TrimRight(entry.Path, "/")
	rp := strings.TrimRight(reqPath, "/")
	return rp == sharePath ||
		strings.HasPrefix(rp, sharePath+"/") ||
		sharePath == ""
}

func loadShares(path string) map[string]shareEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]shareEntry{}
	}
	var m map[string]shareEntry
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]shareEntry{}
	}
	return m
}

func saveShares(path string, shares map[string]shareEntry) error {
	data, err := json.MarshalIndent(shares, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}
