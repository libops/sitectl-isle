package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
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

var (
	endpoint string
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
		urls, err := fetchURLs(cmd.Context(), endpoint)
		if err != nil {
			return fmt.Errorf("error fetching URLs: %v", err)
		}

		return runMiradorWarm(cmd.Context(), urls, workers, func(parent context.Context, url string) error {
			execCtx, cancelExec := chromedp.NewExecAllocator(parent, chromedp.DefaultExecAllocatorOptions[:]...)
			defer cancelExec()

			browserCtx, cancelBrowser := chromedp.NewContext(execCtx)
			defer cancelBrowser()

			return warmURL(browserCtx, url)
		})
	},
}

func fetchURLs(ctx context.Context, endpoint string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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

	var urls []string
	for _, item := range items {
		if item.URL != "" {
			urls = append(urls, item.URL)
		}
	}
	return urls, nil
}

func runMiradorWarm(ctx context.Context, urls []string, workerCount int, warm func(context.Context, string) error) error {
	if workerCount < 1 {
		workerCount = 1
	}

	jobs := make(chan string)
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

	defer func() {
		close(jobs)
		wg.Wait()
	}()

	for _, url := range urls {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- url:
		}
	}

	return nil
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
