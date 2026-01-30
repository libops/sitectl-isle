package islandora

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const cacheDir = "/tmp/islandora"

// getCacheFilename creates a unique filename based on URL
func getCacheFilename(url string) string {
	hash := md5.Sum([]byte(url))
	return filepath.Join(cacheDir, fmt.Sprintf("%x.json", hash))
}

// isCacheValid checks if cache file exists and is less than 1 day old
func isCacheValid(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < 24*time.Hour
}

// ensureCacheDir creates cache directory if it doesn't exist
func ensureCacheDir() error {
	return os.MkdirAll(cacheDir, 0755)
}
func getRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	user := os.Getenv("ISLANDORA_WORKBENCH_USERNAME")
	pass := os.Getenv("ISLANDORA_WORKBENCH_PASSWORD")
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}

	return req, nil
}

func decodeJsonResponse(resp *http.Response, obj any) error {
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status code for %s: %s", resp.Request.URL, resp.Status)
	}

	err := json.NewDecoder(resp.Body).Decode(&obj)
	if err != nil {
		return fmt.Errorf("error decoding JSON response %s: %v", resp.Request.URL, err)
	}

	return nil
}
