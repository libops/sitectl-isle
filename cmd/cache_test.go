package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

func fetchTestURLs(ctx context.Context, endpoint string, limit int) ([]string, error) {
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

func TestFetchTestURLs(t *testing.T) {
	responses := map[int]string{
		0: `[
			{"url":"https://example.test/one"},
			{"url":""}
		]`,
		1: `[
			{"url":"https://example.test/two"}
		]`,
		2: `[]`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		_, _ = w.Write([]byte(responses[page]))
	}))
	defer server.Close()

	got, err := fetchTestURLs(context.Background(), server.URL, 0)
	if err != nil {
		t.Fatalf("fetchTestURLs() error = %v", err)
	}

	want := []string{"https://example.test/one", "https://example.test/two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fetchTestURLs() = %v, want %v", got, want)
	}
}

func TestFetchTestURLsRejectsBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := fetchTestURLs(context.Background(), server.URL, 0)
	if err == nil {
		t.Fatal("fetchTestURLs() error = nil, want status error")
	}
}

func TestFetchTestURLsRespectsLimitAcrossPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		switch page {
		case 0:
			_, _ = w.Write([]byte(`[
				{"url":"https://example.test/one"},
				{"url":"https://example.test/two"}
			]`))
		case 1:
			_, _ = w.Write([]byte(`[
				{"url":"https://example.test/three"}
			]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()

	got, err := fetchTestURLs(context.Background(), server.URL, 2)
	if err != nil {
		t.Fatalf("fetchTestURLs() error = %v", err)
	}

	want := []string{"https://example.test/one", "https://example.test/two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fetchTestURLs() = %v, want %v", got, want)
	}
}

func TestFetchTestURLsStartsFromEndpointPageParam(t *testing.T) {
	var pages []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pages = append(pages, page)
		switch page {
		case 4:
			_, _ = w.Write([]byte(`[
				{"url":"https://example.test/four"}
			]`))
		case 5:
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected page %d", page)
		}
	}))
	defer server.Close()

	got, err := fetchTestURLs(context.Background(), server.URL+"?page=4&_format=json", 0)
	if err != nil {
		t.Fatalf("fetchTestURLs() error = %v", err)
	}

	if !reflect.DeepEqual(got, []string{"https://example.test/four"}) {
		t.Fatalf("fetchTestURLs() = %v, want one result from page 4", got)
	}
	if !slices.Equal(pages, []int{4, 5}) {
		t.Fatalf("pages fetched = %v, want [4 5]", pages)
	}
}

func TestRunMiradorWarmStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	var (
		mu   sync.Mutex
		seen []string
	)

	done := make(chan error, 1)
	go func() {
		done <- runMiradorWarm(ctx, []string{"one", "two", "three"}, 1, func(ctx context.Context, url string) error {
			mu.Lock()
			seen = append(seen, url)
			mu.Unlock()

			select {
			case <-started:
			default:
				close(started)
			}

			<-ctx.Done()
			return ctx.Err()
		})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first warm call")
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runMiradorWarm() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runMiradorWarm to exit after cancel")
	}

	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(seen, []string{"one"}) {
		t.Fatalf("warmed URLs = %v, want only first URL before cancellation", seen)
	}
}

func TestRunMiradorWarmFromEndpointFetchesWhileWarming(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu      sync.Mutex
		fetches []int
		warms   []string
	)

	firstWarmStarted := make(chan struct{})
	releaseFirstWarm := make(chan struct{})
	pageOneFetched := make(chan struct{})

	fetchPage := func(ctx context.Context, endpoint string, page int) ([]URLItem, error) {
		mu.Lock()
		fetches = append(fetches, page)
		mu.Unlock()

		if page == 1 {
			select {
			case <-pageOneFetched:
			default:
				close(pageOneFetched)
			}
		}

		switch page {
		case 0:
			return []URLItem{{URL: "one"}}, nil
		case 1:
			return []URLItem{{URL: "two"}}, nil
		case 2:
			return []URLItem{}, nil
		default:
			return nil, fmt.Errorf("unexpected page %d", page)
		}
	}

	warm := func(ctx context.Context, url string) error {
		mu.Lock()
		warms = append(warms, url)
		mu.Unlock()

		if url == "one" {
			select {
			case <-firstWarmStarted:
			default:
				close(firstWarmStarted)
			}
			<-releaseFirstWarm
		}
		return nil
	}

	done := make(chan error, 1)
	go func() {
		done <- runMiradorWarmFromEndpoint(ctx, "https://example.test/api?_format=json", 0, 1, fetchPage, warm)
	}()

	select {
	case <-firstWarmStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first warm to start")
	}

	select {
	case <-pageOneFetched:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second page fetch while first warm was running")
	}

	close(releaseFirstWarm)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runMiradorWarmFromEndpoint() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runMiradorWarmFromEndpoint to complete")
	}

	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(fetches, []int{0, 1, 2}) {
		t.Fatalf("pages fetched = %v, want [0 1 2]", fetches)
	}
	if !reflect.DeepEqual(warms, []string{"one", "two"}) {
		t.Fatalf("warmed URLs = %v, want [one two]", warms)
	}
}

func TestRunMiradorWarmFromEndpointResumesAfterCancel(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	previousRestart := restartMirador
	restartMirador = false
	t.Cleanup(func() {
		restartMirador = previousRestart
	})

	endpoint := "https://example.test/api?_format=json"
	fetchPage := func(ctx context.Context, endpoint string, page int) ([]URLItem, error) {
		switch page {
		case 0:
			return []URLItem{{URL: "one"}, {URL: "two"}}, nil
		case 1:
			return []URLItem{}, nil
		default:
			return nil, fmt.Errorf("unexpected page %d", page)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var firstRun []string
	err := runMiradorWarmFromEndpoint(ctx, endpoint, 0, 1, fetchPage, func(ctx context.Context, url string) error {
		firstRun = append(firstRun, url)
		if url == "one" {
			cancel()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first run error = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(firstRun, []string{"one"}) {
		t.Fatalf("first run warmed URLs = %v, want [one]", firstRun)
	}

	progressPath, err := miradorProgressPath(endpoint)
	if err != nil {
		t.Fatalf("miradorProgressPath() error = %v", err)
	}
	if _, err := os.Stat(progressPath); err != nil {
		t.Fatalf("progress file stat error = %v, want file to exist", err)
	}

	var secondRun []string
	err = runMiradorWarmFromEndpoint(context.Background(), endpoint, 0, 1, fetchPage, func(ctx context.Context, url string) error {
		secondRun = append(secondRun, url)
		return nil
	})
	if err != nil {
		t.Fatalf("second run error = %v", err)
	}
	if !reflect.DeepEqual(secondRun, []string{"two"}) {
		t.Fatalf("second run warmed URLs = %v, want [two]", secondRun)
	}

	if _, err := os.Stat(progressPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("progress file stat error = %v, want os.ErrNotExist", err)
	}
}

func TestReadMiradorManifestSelectionFromHTML(t *testing.T) {
	htmlText := `<html><head><script type="application/json" data-drupal-selector="drupal-settings-json">{"mirador":{"viewers":{"#mirador-466351":{"id":"mirador-466351","manifests":{"https:\/\/preserve.lehigh.edu\/node\/466351\/book-manifest":{"provider":"Islandora"}},"windows":[{"manifestId":"https:\/\/preserve.lehigh.edu\/node\/466351\/book-manifest"}]}}}}</script></head><body></body></html>`

	got, err := readMiradorManifestSelectionFromHTML(htmlText)
	if err != nil {
		t.Fatalf("readMiradorManifestSelectionFromHTML() error = %v", err)
	}

	want := miradorManifestSelection{
		ViewerID:   "mirador-466351",
		ManifestID: "https://preserve.lehigh.edu/node/466351/book-manifest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readMiradorManifestSelectionFromHTML() = %+v, want %+v", got, want)
	}
}

func TestReadMiradorManifestSelectionFromHTMLPrefersMiradorViewID(t *testing.T) {
	htmlText := `<html><head><script type="application/json" data-drupal-selector="drupal-settings-json">{"mirador":{"viewers":{"#mirador-a":{"id":"mirador-a","windows":[{"manifestId":"https:\/\/example.test\/wrong-manifest"}]},"#mirador-b":{"id":"mirador-b","windows":[{"manifestId":"https:\/\/example.test\/right-manifest"}]}}},"mirador_view_id":"mirador-b"}</script></head><body></body></html>`

	got, err := readMiradorManifestSelectionFromHTML(htmlText)
	if err != nil {
		t.Fatalf("readMiradorManifestSelectionFromHTML() error = %v", err)
	}

	want := miradorManifestSelection{
		ViewerID:   "mirador-b",
		ManifestID: "https://example.test/right-manifest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readMiradorManifestSelectionFromHTML() = %+v, want %+v", got, want)
	}
}

func TestCollectManifestWarmURLsForMode(t *testing.T) {
	manifest := map[string]any{
		"thumbnail": map[string]any{
			"@id": "https://example.test/iiif/3/thumb/root",
		},
		"items": []any{
			map[string]any{
				"thumbnail": map[string]any{
					"@id": "https://example.test/iiif/3/thumb/canvas-1",
				},
				"images": []any{
					map[string]any{
						"resource": map[string]any{
							"@id": "https://example.test/iiif/3/full/1",
						},
					},
				},
			},
			map[string]any{
				"thumbnail": map[string]any{
					"@id": "https://example.test/iiif/3/thumb/canvas-2",
				},
				"images": []any{
					map[string]any{
						"resource": map[string]any{
							"@id": "https://example.test/iiif/3/full/2",
						},
					},
				},
			},
			map[string]any{
				"images": []any{
					map[string]any{
						"resource": map[string]any{
							"@id": "https://example.test/iiif/3/full/3",
						},
					},
				},
			},
		},
	}

	ttfb := collectManifestWarmURLsForMode(manifest, miradorModeTTFB)
	wantTTFB := []string{
		"https://example.test/iiif/3/full/1",
		"https://example.test/iiif/3/thumb/root",
		"https://example.test/iiif/3/thumb/canvas-1",
		"https://example.test/iiif/3/thumb/canvas-2",
	}
	if !reflect.DeepEqual(ttfb, wantTTFB) {
		t.Fatalf("collectManifestWarmURLsForMode(ttfb) = %v, want %v", ttfb, wantTTFB)
	}

	browse := collectManifestWarmURLsForMode(manifest, miradorModeBrowse)
	wantBrowse := []string{
		"https://example.test/iiif/3/full/2",
		"https://example.test/iiif/3/full/3",
	}
	if !reflect.DeepEqual(browse, wantBrowse) {
		t.Fatalf("collectManifestWarmURLsForMode(browse) = %v, want %v", browse, wantBrowse)
	}

	full := collectManifestWarmURLsForMode(manifest, miradorModeFull)
	wantFull := []string{
		"https://example.test/iiif/3/full/1",
		"https://example.test/iiif/3/thumb/root",
		"https://example.test/iiif/3/thumb/canvas-1",
		"https://example.test/iiif/3/thumb/canvas-2",
		"https://example.test/iiif/3/full/2",
		"https://example.test/iiif/3/full/3",
	}
	if !reflect.DeepEqual(full, wantFull) {
		t.Fatalf("collectManifestWarmURLsForMode(full) = %v, want %v", full, wantFull)
	}
}

func TestCollectManifestWarmURLsForModeThumbnailSplit(t *testing.T) {
	var thumbnails []any
	for i := 1; i <= 12; i++ {
		thumbnails = append(thumbnails, map[string]any{
			"@id": fmt.Sprintf("https://example.test/iiif/3/thumb/%d", i),
		})
	}

	manifest := map[string]any{
		"thumbnail": thumbnails,
		"images": []any{
			map[string]any{
				"resource": map[string]any{
					"@id": "https://example.test/iiif/3/full/1",
				},
			},
			map[string]any{
				"resource": map[string]any{
					"@id": "https://example.test/iiif/3/full/2",
				},
			},
		},
	}

	ttfb := collectManifestWarmURLsForMode(manifest, miradorModeTTFB)
	wantTTFB := []string{
		"https://example.test/iiif/3/full/1",
		"https://example.test/iiif/3/thumb/1",
		"https://example.test/iiif/3/thumb/2",
		"https://example.test/iiif/3/thumb/3",
		"https://example.test/iiif/3/thumb/4",
		"https://example.test/iiif/3/thumb/5",
		"https://example.test/iiif/3/thumb/6",
		"https://example.test/iiif/3/thumb/7",
		"https://example.test/iiif/3/thumb/8",
		"https://example.test/iiif/3/thumb/9",
		"https://example.test/iiif/3/thumb/10",
	}
	if !reflect.DeepEqual(ttfb, wantTTFB) {
		t.Fatalf("collectManifestWarmURLsForMode(ttfb, thumbnail split) = %v, want %v", ttfb, wantTTFB)
	}

	browse := collectManifestWarmURLsForMode(manifest, miradorModeBrowse)
	wantBrowse := []string{
		"https://example.test/iiif/3/full/2",
		"https://example.test/iiif/3/thumb/11",
		"https://example.test/iiif/3/thumb/12",
	}
	if !reflect.DeepEqual(browse, wantBrowse) {
		t.Fatalf("collectManifestWarmURLsForMode(browse, thumbnail split) = %v, want %v", browse, wantBrowse)
	}
}

func TestCollectManifestWarmURLsForModePresentation3(t *testing.T) {
	manifest := map[string]any{
		"thumbnail": []any{
			map[string]any{"id": "https://example.test/iiif/3/thumb/root"},
		},
		"items": []any{
			map[string]any{
				"thumbnail": map[string]any{
					"id": "https://example.test/iiif/3/thumb/canvas-1",
				},
				"items": []any{
					map[string]any{
						"items": []any{
							map[string]any{
								"body": map[string]any{
									"id": "https://example.test/iiif/3/full/1",
								},
							},
						},
					},
				},
			},
			map[string]any{
				"thumbnail": map[string]any{
					"id": "https://example.test/iiif/3/thumb/canvas-2",
				},
				"items": []any{
					map[string]any{
						"items": []any{
							map[string]any{
								"body": map[string]any{
									"id": "https://example.test/iiif/3/full/2",
								},
							},
						},
					},
				},
			},
		},
	}

	ttfb := collectManifestWarmURLsForMode(manifest, miradorModeTTFB)
	wantTTFB := []string{
		"https://example.test/iiif/3/full/1",
		"https://example.test/iiif/3/thumb/root",
		"https://example.test/iiif/3/thumb/canvas-1",
		"https://example.test/iiif/3/thumb/canvas-2",
	}
	if !reflect.DeepEqual(ttfb, wantTTFB) {
		t.Fatalf("collectManifestWarmURLsForMode(ttfb, p3) = %v, want %v", ttfb, wantTTFB)
	}

	browse := collectManifestWarmURLsForMode(manifest, miradorModeBrowse)
	wantBrowse := []string{
		"https://example.test/iiif/3/full/2",
	}
	if !reflect.DeepEqual(browse, wantBrowse) {
		t.Fatalf("collectManifestWarmURLsForMode(browse, p3) = %v, want %v", browse, wantBrowse)
	}
}

func TestReadMiradorURLsFromFile(t *testing.T) {
	path := t.TempDir() + "/urls.txt"
	if err := os.WriteFile(path, []byte("https://example.test/one\n\n https://example.test/two \nhttps://example.test/three\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := readMiradorURLsFromFile(path, 2)
	if err != nil {
		t.Fatalf("readMiradorURLsFromFile() error = %v", err)
	}

	want := []string{
		"https://example.test/one",
		"https://example.test/two",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readMiradorURLsFromFile() = %v, want %v", got, want)
	}
}
