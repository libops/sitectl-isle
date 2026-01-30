package islandora

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"github.com/libops/sitectl-drupal/model"
)

type Term struct {
	Name model.GenericField `json:"name"`
}

func FetchTerm(url string) (model.TermResponse, error) {
	cacheFile := getCacheFilename(url)
	var term model.TermResponse

	// Try to read from cache first
	if isCacheValid(cacheFile) {
		data, err := os.ReadFile(cacheFile)
		if err == nil {
			if json.Unmarshal(data, &term) == nil {
				return term, nil
			}
		}
	}

	req, err := getRequest(url)
	if err != nil {
		return term, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return term, err
	}

	err = decodeJsonResponse(resp, &term)
	if err != nil {
		return term, err
	}

	// Cache the result
	if err := ensureCacheDir(); err == nil {
		if data, err := json.Marshal(term); err == nil {
			err = os.WriteFile(cacheFile, data, 0644)
			if err != nil {
				slog.Error("Unable to write file", "file", cacheFile, "err", err)
			}
		}
	}

	return term, nil
}
