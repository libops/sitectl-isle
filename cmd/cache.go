package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/spf13/cobra"
)

type URLItem struct {
	URL string `json:"url"`
}

type miradorStateSummary struct {
	ViewerID      string `json:"viewerId"`
	ManifestCount int    `json:"manifestCount"`
	WindowCount   int    `json:"windowCount"`
	ReadyWindows  int    `json:"readyWindows"`
}

type miradorRunStats struct {
	pagesFetched atomic.Int64
	urlsQueued   atomic.Int64
	urlsWarmed   atomic.Int64
	urlsFailed   atomic.Int64
}

var (
	endpoint string
	limit    int
	workers  int
)

// cacheCmd represents the transform command
var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Cache warmer commands to speed up your ISLE site page load times",
}

func init() {
	cacheCmd.AddCommand(cacheMirador)

	cacheMirador.Flags().StringVar(&endpoint, "endpoint", "", "JSON endpoint returning an array of objects with a url field. Required.")
	cacheMirador.Flags().IntVar(&limit, "limit", 0, "Maximum number of URLs to warm. Use 0 to paginate through all endpoint pages.")
	cacheMirador.Flags().IntVar(&workers, "workers", 2, "Number of concurrent browser workers.")
	must(cacheMirador.MarkFlagRequired("endpoint"))
}

// cacheMirador represents the mirador command
var cacheMirador = &cobra.Command{
	Use:   "mirador",
	Short: "Pre-warm the Mirador IIIF viewer image cache",
	Long: `Pre-warm the Mirador IIIF viewer cache for paged content items.

When the IIIF server has not yet cached a paged item's child images, the first visitor to that
	page experiences slow load times while the server processes the images. This command fetches a
	list of paged content URLs from a JSON endpoint and renders each one in a headless browser so
	the IIIF server caches the images before real visitors arrive.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMiradorWarmFromEndpoint(cmd.Context(), endpoint, limit, workers, fetchURLPage, func(parent context.Context, url string) error {
			execCtx, cancelExec := chromedp.NewExecAllocator(parent, chromedp.DefaultExecAllocatorOptions[:]...)
			defer cancelExec()

			browserCtx, cancelBrowser := chromedp.NewContext(execCtx)
			defer cancelBrowser()

			return warmURL(browserCtx, url)
		})
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

	resp, err := http.DefaultClient.Do(req)
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
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint failed: %w", err)
	}

	query := parsed.Query()
	query.Set("page", strconv.Itoa(page))
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func runMiradorWarmFromEndpoint(
	ctx context.Context,
	endpoint string,
	limit int,
	workerCount int,
	fetchPage func(context.Context, string, int) ([]URLItem, error),
	warm func(context.Context, string) error,
) error {
	startedAt := time.Now()
	stats := &miradorRunStats{}
	var resultErr error
	defer func() {
		slog.Info(
			"Mirador warm summary",
			"endpoint", endpoint,
			"pages_fetched", stats.pagesFetched.Load(),
			"urls_queued", stats.urlsQueued.Load(),
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
		producerErr <- enqueueMiradorURLs(ctx, jobs, endpoint, limit, func(ctx context.Context, endpoint string, page int) ([]URLItem, error) {
			items, err := fetchPage(ctx, endpoint, page)
			if err == nil {
				stats.pagesFetched.Add(1)
			}
			return items, err
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
		return nil
	})
	fetchErr := <-producerErr

	if fetchErr != nil && !errors.Is(fetchErr, context.Canceled) {
		resultErr = fetchErr
		return resultErr
	}

	resultErr = workerErr
	return resultErr
}

func fetchURLs(ctx context.Context, endpoint string, limit int) ([]string, error) {
	var urls []string
	err := enqueueMiradorURLs(ctx, nil, endpoint, limit, func(ctx context.Context, endpoint string, page int) ([]URLItem, error) {
		return fetchURLPage(ctx, endpoint, page)
	}, func(item URLItem) {
		if item.URL != "" {
			urls = append(urls, item.URL)
		}
	})
	if err != nil {
		return nil, err
	}
	return urls, nil
}

func enqueueMiradorURLs(
	ctx context.Context,
	jobs chan<- string,
	endpoint string,
	limit int,
	fetchPage func(context.Context, string, int) ([]URLItem, error),
	collect ...func(URLItem),
) error {
	nextPage, err := startingPage(endpoint)
	if err != nil {
		return err
	}

	sent := 0
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
			sent++
			if limit > 0 && sent >= limit {
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
					if err := warm(ctx, url); err != nil {
						if ctx.Err() != nil {
							return
						}
						slog.Error("worker Failed", "url", url, "err", err)
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

	localCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var state miradorStateSummary
	tasks := chromedp.Tasks{
		chromedp.Navigate(url),
		chromedp.Poll(`
			(() => {
				const node = document.querySelector('.block-mirador[data-once*="mirador-viewer"]');
				if (!node || !node.id) return false;
				if (!window.Drupal || !Drupal.IslandoraMirador || !Drupal.IslandoraMirador.instances) return false;
				const instance = Drupal.IslandoraMirador.instances['#' + node.id];
				if (!instance || !instance.store || typeof instance.store.getState !== 'function') return false;
				const state = instance.store.getState();
				if (!state.manifests || Object.keys(state.manifests).length === 0) return false;
				if (!state.windows || Object.keys(state.windows).length === 0) return false;
				return Object.values(state.windows).some((win) => win && win.canvasId && win.manifestId);
			})()`, nil),
		chromedp.Evaluate(`
			(() => {
				const node = document.querySelector('.block-mirador[data-once*="mirador-viewer"]');
				if (!node || !node.id || !window.Drupal || !Drupal.IslandoraMirador || !Drupal.IslandoraMirador.instances) {
					return { viewerId: "", manifestCount: 0, windowCount: 0, readyWindows: 0 };
				}
				const instance = Drupal.IslandoraMirador.instances['#' + node.id];
				if (!instance || !instance.store || typeof instance.store.getState !== 'function') {
					return { viewerId: node.id, manifestCount: 0, windowCount: 0, readyWindows: 0 };
				}
				const state = instance.store.getState();
				const manifests = state.manifests ? Object.keys(state.manifests).length : 0;
				const windows = state.windows ? Object.values(state.windows) : [];
				const readyWindows = windows.filter((win) => win && win.canvasId && win.manifestId).length;
				return {
					viewerId: node.id,
					manifestCount: manifests,
					windowCount: windows.length,
					readyWindows: readyWindows
				};
			})()`, &state),
	}

	if err := chromedp.Run(localCtx, tasks); err != nil {
		return err
	}

	slog.Info(
		"Mirador ready",
		"url", url,
		"viewer_id", state.ViewerID,
		"manifest_count", state.ManifestCount,
		"window_count", state.WindowCount,
		"ready_windows", state.ReadyWindows,
	)

	return nil
}
