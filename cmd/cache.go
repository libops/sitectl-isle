package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
)

type URLItem struct {
	URL string `json:"url"`
}

type miradorManifestSelection struct {
	ViewerID   string `json:"viewerId"`
	ManifestID string `json:"manifestId"`
}

const (
	miradorModeTTFB           = "ttfb"
	miradorModeBrowse         = "browse"
	miradorModeFull           = "full"
	miradorThumbnailTTFBCount = 10
)

type miradorRunStats struct {
	pagesFetched atomic.Int64
	urlsQueued   atomic.Int64
	urlsSkipped  atomic.Int64
	urlsWarmed   atomic.Int64
	urlsFailed   atomic.Int64
}

type miradorProgressState struct {
	Endpoint      string   `json:"endpoint"`
	CompletedURLs []string `json:"completed_urls"`
}

type miradorProgressTracker struct {
	path      string
	endpoint  string
	mu        sync.Mutex
	completed map[string]struct{}
}

var (
	endpoint       string
	urlFile        string
	limit          int
	workers        int
	delay          time.Duration
	miradorMode    string
	restartMirador bool
)

// cacheCmd represents the transform command
var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Cache warmer commands to speed up your ISLE site page load times",
}

func init() {
	cacheCmd.AddCommand(cacheMirador)

	cacheMirador.Flags().StringVar(&endpoint, "endpoint", "", "JSON endpoint returning an array of objects with a url field. Mutually exclusive with --url-file.")
	cacheMirador.Flags().StringVar(&urlFile, "url-file", "", "Path to a file containing one URL per line. Mutually exclusive with --endpoint.")
	cacheMirador.Flags().IntVar(&limit, "limit", 0, "Maximum number of URLs to warm. Use 0 to paginate through all endpoint pages.")
	cacheMirador.Flags().IntVar(&workers, "workers", 1, "Number of concurrent warm workers.")
	cacheMirador.Flags().DurationVar(&delay, "delay", time.Second, "How long to sleep between HTTP warm requests.")
	cacheMirador.Flags().StringVar(&miradorMode, "mode", miradorModeTTFB, "Mirador warm mode: ttfb, browse, or full.")
	cacheMirador.Flags().BoolVar(&restartMirador, "restart", false, "Discard saved Mirador warm progress and restart from the beginning.")
}

// cacheMirador represents the mirador command
var cacheMirador = &cobra.Command{
	Use:   "mirador",
	Short: "Pre-warm the Mirador IIIF viewer image cache",
	Long: `Pre-warm the Mirador IIIF viewer cache for paged content items.

When the IIIF server has not yet cached a paged item's child images, the first visitor to that
	page experiences slow load times while the server processes the images. This command fetches a
	list of paged content URLs from either a JSON endpoint or a file, reads the Mirador settings
	embedded in each page's Drupal settings JSON, and warms the related IIIF resources before real
	visitors arrive.

Pass exactly one of:
  --endpoint   JSON endpoint returning URL objects
  --url-file   text file with one URL per line`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if miradorMode != miradorModeTTFB && miradorMode != miradorModeBrowse && miradorMode != miradorModeFull {
			return fmt.Errorf("invalid --mode %q: must be %q, %q, or %q", miradorMode, miradorModeTTFB, miradorModeBrowse, miradorModeFull)
		}
		switch {
		case endpoint != "" && urlFile != "":
			return fmt.Errorf("use either --endpoint or --url-file, not both")
		case endpoint == "" && urlFile == "":
			return fmt.Errorf("either --endpoint or --url-file is required")
		case urlFile != "":
			sourceKey, err := miradorURLFileSourceKey(urlFile)
			if err != nil {
				return err
			}
			urls, err := readMiradorURLsFromFile(urlFile, limit)
			if err != nil {
				return err
			}
			return runMiradorWarmFromURLs(cmd.Context(), sourceKey, urls, workers, warmURL)
		default:
			return runMiradorWarmFromEndpoint(cmd.Context(), endpoint, limit, workers, fetchURLPage, warmURL)
		}
	},
}

func startingPage(endpoint string) (int, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return 0, fmt.Errorf("parse endpoint failed: %w", err)
	}

	pageValue := parsed.Query().Get("page")
	if pageValue == "" {
		return 0, nil
	}

	page, err := strconv.Atoi(pageValue)
	if err != nil {
		return 0, fmt.Errorf("parse page failed: %w", err)
	}

	return page, nil
}

func fetchURLPage(ctx context.Context, endpoint string, page int) ([]URLItem, error) {
	pageURL, err := endpointPageURL(endpoint, page)
	if err != nil {
		return nil, err
	}

	slog.Info("Fetching Mirador endpoint page", "page", page, "endpoint", pageURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request failed: %w", err)
	}

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GET failed: unexpected status %s", resp.Status)
	}

	var items []URLItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode failed: %w", err)
	}

	return items, nil
}

func endpointPageURL(endpoint string, page int) (string, error) {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint failed: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("endpoint must use http or https")
	}

	query := parsed.Query()
	query.Set("page", strconv.Itoa(page))
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func runMiradorWarmFromEndpoint(
	ctx context.Context,
	sourceKey string,
	limit int,
	workerCount int,
	fetchPage func(context.Context, string, int) ([]URLItem, error),
	warm func(context.Context, string) error,
) error {
	startedAt := time.Now()
	stats := &miradorRunStats{}
	progress, err := loadMiradorProgressTracker(sourceKey, restartMirador)
	if err != nil {
		return err
	}

	if progress.Count() > 0 {
		slog.Info("Resuming Mirador warm progress", "progress_file", progress.path, "completed_urls", progress.Count())
	}

	var resultErr error
	defer func() {
		slog.Info(
			"Mirador warm summary",
			"source", sourceKey,
			"pages_fetched", stats.pagesFetched.Load(),
			"urls_queued", stats.urlsQueued.Load(),
			"urls_skipped", stats.urlsSkipped.Load(),
			"urls_warmed", stats.urlsWarmed.Load(),
			"urls_failed", stats.urlsFailed.Load(),
			"elapsed", time.Since(startedAt).Round(time.Millisecond).String(),
			"canceled", errors.Is(resultErr, context.Canceled),
		)
	}()

	jobs := make(chan string, max(1, workerCount))
	producerErr := make(chan error, 1)

	go func() {
		defer close(jobs)
		producerErr <- enqueueMiradorURLs(ctx, jobs, sourceKey, limit, func(ctx context.Context, source string, page int) ([]URLItem, error) {
			items, err := fetchPage(ctx, source, page)
			if err == nil {
				stats.pagesFetched.Add(1)
			}
			return items, err
		}, func(item URLItem) bool {
			skip := progress.Has(item.URL)
			if skip {
				stats.urlsSkipped.Add(1)
			}
			return skip
		}, func(item URLItem) {
			if item.URL != "" {
				stats.urlsQueued.Add(1)
			}
		})
	}()

	workerErr := runMiradorWorkers(ctx, jobs, workerCount, func(ctx context.Context, url string) error {
		err := warm(ctx, url)
		if err != nil {
			if ctx.Err() == nil {
				stats.urlsFailed.Add(1)
			}
			return err
		}

		stats.urlsWarmed.Add(1)
		if err := progress.MarkCompleted(url); err != nil {
			return err
		}
		return nil
	})
	fetchErr := <-producerErr

	if fetchErr != nil && !errors.Is(fetchErr, context.Canceled) {
		resultErr = fetchErr
		return resultErr
	}

	if workerErr != nil {
		resultErr = workerErr
		return resultErr
	}

	if stats.urlsFailed.Load() > 0 {
		resultErr = fmt.Errorf("%d URLs failed during Mirador warming; progress retained at %s", stats.urlsFailed.Load(), progress.path)
		return resultErr
	}

	if err := progress.Remove(); err != nil {
		resultErr = err
		return resultErr
	}

	resultErr = nil
	return resultErr
}

func runMiradorWarmFromURLs(
	ctx context.Context,
	sourceKey string,
	urls []string,
	workerCount int,
	warm func(context.Context, string) error,
) error {
	startedAt := time.Now()
	stats := &miradorRunStats{}
	progress, err := loadMiradorProgressTracker(sourceKey, restartMirador)
	if err != nil {
		return err
	}

	if progress.Count() > 0 {
		slog.Info("Resuming Mirador warm progress", "progress_file", progress.path, "completed_urls", progress.Count())
	}

	var resultErr error
	defer func() {
		slog.Info(
			"Mirador warm summary",
			"source", sourceKey,
			"pages_fetched", stats.pagesFetched.Load(),
			"urls_queued", stats.urlsQueued.Load(),
			"urls_skipped", stats.urlsSkipped.Load(),
			"urls_warmed", stats.urlsWarmed.Load(),
			"urls_failed", stats.urlsFailed.Load(),
			"elapsed", time.Since(startedAt).Round(time.Millisecond).String(),
			"canceled", errors.Is(resultErr, context.Canceled),
		)
	}()

	var queued []string
	for _, item := range urls {
		if item == "" {
			continue
		}
		if progress.Has(item) {
			stats.urlsSkipped.Add(1)
			continue
		}
		stats.urlsQueued.Add(1)
		queued = append(queued, item)
	}

	workerErr := runMiradorWarm(ctx, queued, workerCount, func(ctx context.Context, url string) error {
		err := warm(ctx, url)
		if err != nil {
			if ctx.Err() == nil {
				stats.urlsFailed.Add(1)
			}
			return err
		}

		stats.urlsWarmed.Add(1)
		if err := progress.MarkCompleted(url); err != nil {
			return err
		}
		return nil
	})

	if workerErr != nil {
		resultErr = workerErr
		return resultErr
	}

	if stats.urlsFailed.Load() > 0 {
		resultErr = fmt.Errorf("%d URLs failed during Mirador warming; progress retained at %s", stats.urlsFailed.Load(), progress.path)
		return resultErr
	}

	if err := progress.Remove(); err != nil {
		resultErr = err
		return resultErr
	}

	resultErr = nil
	return resultErr
}

func fetchURLs(ctx context.Context, endpoint string, limit int) ([]string, error) {
	var urls []string
	err := enqueueMiradorURLs(ctx, nil, endpoint, limit, func(ctx context.Context, endpoint string, page int) ([]URLItem, error) {
		return fetchURLPage(ctx, endpoint, page)
	}, nil, func(item URLItem) {
		if item.URL != "" {
			urls = append(urls, item.URL)
		}
	})
	if err != nil {
		return nil, err
	}
	return urls, nil
}

func readMiradorURLsFromFile(path string, limit int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read url file failed: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	urls := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		urls = append(urls, trimmed)
		if limit > 0 && len(urls) >= limit {
			break
		}
	}

	return urls, nil
}

func miradorURLFileSourceKey(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve url file path failed: %w", err)
	}
	return "file:" + absPath, nil
}

func enqueueMiradorURLs(
	ctx context.Context,
	jobs chan<- string,
	endpoint string,
	limit int,
	fetchPage func(context.Context, string, int) ([]URLItem, error),
	shouldSkip func(URLItem) bool,
	collect ...func(URLItem),
) error {
	nextPage, err := startingPage(endpoint)
	if err != nil {
		return err
	}

	seen := 0
	for {
		items, err := fetchPage(ctx, endpoint, nextPage)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}

		for _, item := range items {
			if item.URL == "" {
				continue
			}
			seen++
			if shouldSkip != nil && shouldSkip(item) {
				if limit > 0 && seen >= limit {
					return nil
				}
				continue
			}
			if jobs != nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case jobs <- item.URL:
				}
			}
			for _, fn := range collect {
				fn(item)
			}
			if limit > 0 && seen >= limit {
				return nil
			}
		}

		nextPage++
	}
}

func runMiradorWarm(ctx context.Context, urls []string, workerCount int, warm func(context.Context, string) error) error {
	jobs := make(chan string, max(1, workerCount))

	go func() {
		defer close(jobs)
		for _, url := range urls {
			select {
			case <-ctx.Done():
				return
			case jobs <- url:
			}
		}
	}()

	return runMiradorWorkers(ctx, jobs, workerCount, warm)
}

func runMiradorWorkers(ctx context.Context, jobs <-chan string, workerCount int, warm func(context.Context, string) error) error {
	if workerCount < 1 {
		workerCount = 1
	}

	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case url, ok := <-jobs:
					if !ok {
						return
					}
					err := warm(ctx, url)
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						slog.Error("worker Failed", "url", url, "err", err)
						continue
					}
					if ctx.Err() != nil {
						return
					}
				}
			}
		}()
	}

	wg.Wait()
	return ctx.Err()
}

func warmURL(ctx context.Context, url string) error {
	slog.Info("Warming URL", "url", url)

	localCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	htmlText, err := fetchPageHTML(localCtx, url)
	if err != nil {
		return err
	}

	selection, err := readMiradorManifestSelectionFromHTML(htmlText)
	if err != nil {
		return err
	}

	warmedIIIF, err := warmManifestIIIFResources(localCtx, selection.ManifestID)
	if err != nil {
		return err
	}

	slog.Info(
		"Mirador ready",
		"url", url,
		"viewer_id", selection.ViewerID,
		"manifest_id", selection.ManifestID,
		"iiif_urls_warmed", warmedIIIF,
	)

	return nil
}

func fetchPageHTML(ctx context.Context, pageURL string) (string, error) {
	slog.Debug("Fetching page HTML", "url", pageURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", fmt.Errorf("build page request failed: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET page failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("GET page failed: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read page failed: %w", err)
	}

	if err := sleepWithContext(ctx, delay); err != nil {
		return "", err
	}

	return string(body), nil
}

func readMiradorManifestSelectionFromHTML(htmlText string) (miradorManifestSelection, error) {
	settingsJSON, err := extractDrupalSettingsJSON(htmlText)
	if err != nil {
		return miradorManifestSelection{}, err
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		return miradorManifestSelection{}, fmt.Errorf("decode drupal settings failed: %w", err)
	}

	miradorSettings, ok := settings["mirador"].(map[string]any)
	if !ok {
		return miradorManifestSelection{}, fmt.Errorf("could not find mirador settings in drupal settings")
	}

	viewers, ok := miradorSettings["viewers"].(map[string]any)
	if !ok || len(viewers) == 0 {
		return miradorManifestSelection{}, fmt.Errorf("could not find mirador viewers in drupal settings")
	}

	if preferredViewerID, _ := settings["mirador_view_id"].(string); preferredViewerID != "" {
		if selection, ok := readMiradorManifestSelectionFromViewer(viewers, preferredViewerID); ok {
			return selection, nil
		}
	}

	for viewerSelector := range viewers {
		selection, ok := readMiradorManifestSelectionFromViewer(viewers, strings.TrimPrefix(viewerSelector, "#"))
		if ok {
			return selection, nil
		}
	}

	return miradorManifestSelection{}, fmt.Errorf("could not determine Mirador manifest id")
}

func readMiradorManifestSelectionFromViewer(viewers map[string]any, viewerID string) (miradorManifestSelection, bool) {
	for viewerSelector, rawViewer := range viewers {
		viewer, ok := rawViewer.(map[string]any)
		if !ok {
			continue
		}

		candidateViewerID, _ := viewer["id"].(string)
		if candidateViewerID == "" {
			candidateViewerID = strings.TrimPrefix(viewerSelector, "#")
		}
		if candidateViewerID != viewerID {
			continue
		}

		if windows, ok := viewer["windows"].([]any); ok {
			for _, rawWindow := range windows {
				window, ok := rawWindow.(map[string]any)
				if !ok {
					continue
				}
				if manifestID, _ := window["manifestId"].(string); manifestID != "" {
					return miradorManifestSelection{ViewerID: candidateViewerID, ManifestID: manifestID}, true
				}
			}
		}

		if manifests, ok := viewer["manifests"].(map[string]any); ok {
			for manifestID := range manifests {
				if manifestID != "" {
					return miradorManifestSelection{ViewerID: candidateViewerID, ManifestID: manifestID}, true
				}
			}
		}
	}

	return miradorManifestSelection{}, false
}

func extractDrupalSettingsJSON(htmlText string) (string, error) {
	const marker = `data-drupal-selector="drupal-settings-json"`

	start := strings.Index(htmlText, marker)
	if start == -1 {
		return "", fmt.Errorf("could not find drupal-settings-json script")
	}

	open := strings.Index(htmlText[start:], ">")
	if open == -1 {
		return "", fmt.Errorf("could not find drupal-settings-json script start")
	}
	open += start + 1

	close := strings.Index(strings.ToLower(htmlText[open:]), "</script>")
	if close == -1 {
		return "", fmt.Errorf("could not find drupal-settings-json script end")
	}
	close += open

	return strings.TrimSpace(htmlText[open:close]), nil
}

func warmManifestIIIFResources(ctx context.Context, manifestURL string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build manifest request failed: %w", err)
	}
	slog.Debug("Fetching IIIF manifest", "manifest_url", manifestURL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("GET manifest failed: %w", err)
	}
	defer resp.Body.Close()
	slog.Debug("Fetched IIIF manifest", "manifest_url", manifestURL, "status", resp.Status)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("GET manifest failed: unexpected status %s", resp.Status)
	}

	var manifest any
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return 0, fmt.Errorf("decode manifest failed: %w", err)
	}

	urls := collectManifestWarmURLsForMode(manifest, miradorMode)
	for index, iiifURL := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, iiifURL, nil)
		if err != nil {
			return 0, fmt.Errorf("build iiif request failed for %s: %w", iiifURL, err)
		}
		slog.Debug("HEAD IIIF asset", "url", iiifURL, "request_number", index+1, "request_total", len(urls))

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, fmt.Errorf("GET iiif url failed for %s: %w", iiifURL, err)
		}
		resp.Body.Close()
		slog.Debug("HEAD IIIF asset complete", "url", iiifURL, "status", resp.Status, "request_number", index+1, "request_total", len(urls))
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return 0, fmt.Errorf("GET iiif url failed for %s: unexpected status %s", iiifURL, resp.Status)
		}
		if err := sleepWithContext(ctx, delay); err != nil {
			return 0, err
		}
	}

	return len(urls), nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func loadMiradorProgressTracker(endpoint string, restart bool) (*miradorProgressTracker, error) {
	path, err := miradorProgressPath(endpoint)
	if err != nil {
		return nil, err
	}

	if restart {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove Mirador progress file failed: %w", err)
		}
	}

	tracker := &miradorProgressTracker{
		path:      path,
		endpoint:  endpoint,
		completed: make(map[string]struct{}),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return tracker, nil
		}
		return nil, fmt.Errorf("read Mirador progress file failed: %w", err)
	}

	var state miradorProgressState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode Mirador progress file failed: %w", err)
	}

	for _, completedURL := range state.CompletedURLs {
		if completedURL != "" {
			tracker.completed[completedURL] = struct{}{}
		}
	}

	return tracker, nil
}

func miradorProgressPath(endpoint string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory failed: %w", err)
	}

	sum := sha256.Sum256([]byte(endpoint))
	dir := filepath.Join(home, ".sitectl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create Mirador progress directory failed: %w", err)
	}

	return filepath.Join(dir, fmt.Sprintf("mirador-cache-%x.json", sum[:8])), nil
}

func (t *miradorProgressTracker) Has(url string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	_, ok := t.completed[url]
	return ok
}

func (t *miradorProgressTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return len(t.completed)
}

func (t *miradorProgressTracker) MarkCompleted(url string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.completed[url]; ok {
		return nil
	}
	t.completed[url] = struct{}{}

	if err := t.persistLocked(); err != nil {
		delete(t.completed, url)
		return err
	}

	return nil
}

func (t *miradorProgressTracker) Remove() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := os.Remove(t.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Mirador progress file failed: %w", err)
	}

	return nil
}

func (t *miradorProgressTracker) persistLocked() error {
	completed := make([]string, 0, len(t.completed))
	for completedURL := range t.completed {
		completed = append(completed, completedURL)
	}

	state := miradorProgressState{
		Endpoint:      t.endpoint,
		CompletedURLs: completed,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Mirador progress failed: %w", err)
	}

	tmpPath := t.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write Mirador progress failed: %w", err)
	}
	if err := os.Rename(tmpPath, t.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace Mirador progress failed: %w", err)
	}

	return nil
}

func collectManifestWarmURLsForMode(value any, mode string) []string {
	var firstImageURL string
	var otherImageURLs []string
	var thumbnailURLs []string
	seenImages := make(map[string]struct{})
	seenThumbnails := make(map[string]struct{})

	addImageURL := func(s string) {
		if s == "" || !strings.Contains(s, "/iiif/3/") {
			return
		}
		if _, exists := seenImages[s]; exists {
			return
		}
		seenImages[s] = struct{}{}
		if firstImageURL == "" {
			firstImageURL = s
			return
		}
		otherImageURLs = append(otherImageURLs, s)
	}

	addThumbnailURL := func(s string) {
		if s == "" || !strings.Contains(s, "/iiif/3/") {
			return
		}
		if _, exists := seenThumbnails[s]; exists {
			return
		}
		seenThumbnails[s] = struct{}{}
		thumbnailURLs = append(thumbnailURLs, s)
	}

	var addURLFromIDField func(v any, add func(string))
	addURLFromIDField = func(v any, add func(string)) {
		switch typed := v.(type) {
		case map[string]any:
			if s, ok := typed["@id"].(string); ok {
				add(s)
				return
			}
			if s, ok := typed["id"].(string); ok {
				add(s)
			}
		case []any:
			for _, item := range typed {
				addURLFromIDField(item, add)
			}
		}
	}

	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			if thumbnail, ok := typed["thumbnail"]; ok {
				addURLFromIDField(thumbnail, addThumbnailURL)
			}
			if images, ok := typed["images"].([]any); ok {
				for _, image := range images {
					m, ok := image.(map[string]any)
					if !ok {
						continue
					}
					resource, ok := m["resource"].(map[string]any)
					if !ok {
						continue
					}
					if s, ok := resource["@id"].(string); ok {
						addImageURL(s)
					}
				}
			}
			if items, ok := typed["items"].([]any); ok {
				for _, item := range items {
					itemMap, ok := item.(map[string]any)
					if !ok {
						continue
					}
					if annotationPages, ok := itemMap["items"].([]any); ok {
						for _, annotationPage := range annotationPages {
							annotationPageMap, ok := annotationPage.(map[string]any)
							if !ok {
								continue
							}
							annotations, ok := annotationPageMap["items"].([]any)
							if !ok {
								continue
							}
							for _, annotation := range annotations {
								annotationMap, ok := annotation.(map[string]any)
								if !ok {
									continue
								}
								body, ok := annotationMap["body"]
								if !ok {
									continue
								}
								addURLFromIDField(body, addImageURL)
							}
						}
					}
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}

	walk(value)

	switch mode {
	case miradorModeFull:
		var urls []string
		if firstImageURL != "" {
			urls = append(urls, firstImageURL)
		}
		urls = append(urls, thumbnailURLs...)
		urls = append(urls, otherImageURLs...)
		return urls
	case miradorModeBrowse:
		var urls []string
		urls = append(urls, otherImageURLs...)
		if len(thumbnailURLs) > miradorThumbnailTTFBCount {
			urls = append(urls, thumbnailURLs[miradorThumbnailTTFBCount:]...)
		}
		return urls
	case miradorModeTTFB:
		var urls []string
		if firstImageURL != "" {
			urls = append(urls, firstImageURL)
		}
		if len(thumbnailURLs) > miradorThumbnailTTFBCount {
			urls = append(urls, thumbnailURLs[:miradorThumbnailTTFBCount]...)
		} else {
			urls = append(urls, thumbnailURLs...)
		}
		return urls
	default:
		var urls []string
		if firstImageURL != "" {
			urls = append(urls, firstImageURL)
		}
		if len(thumbnailURLs) > miradorThumbnailTTFBCount {
			urls = append(urls, thumbnailURLs[:miradorThumbnailTTFBCount]...)
		} else {
			urls = append(urls, thumbnailURLs...)
		}
		return urls
	}
}
