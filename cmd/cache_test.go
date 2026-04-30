package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestFetchURLs(t *testing.T) {
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

	got, err := fetchURLs(context.Background(), server.URL, 0)
	if err != nil {
		t.Fatalf("fetchURLs() error = %v", err)
	}

	want := []string{"https://example.test/one", "https://example.test/two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fetchURLs() = %v, want %v", got, want)
	}
}

func TestFetchURLsRejectsBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := fetchURLs(context.Background(), server.URL, 0)
	if err == nil {
		t.Fatal("fetchURLs() error = nil, want status error")
	}
}

func TestFetchURLsRespectsLimitAcrossPages(t *testing.T) {
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

	got, err := fetchURLs(context.Background(), server.URL, 2)
	if err != nil {
		t.Fatalf("fetchURLs() error = %v", err)
	}

	want := []string{"https://example.test/one", "https://example.test/two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fetchURLs() = %v, want %v", got, want)
	}
}

func TestFetchURLsStartsFromEndpointPageParam(t *testing.T) {
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

	got, err := fetchURLs(context.Background(), server.URL+"?page=4&_format=json", 0)
	if err != nil {
		t.Fatalf("fetchURLs() error = %v", err)
	}

	if !reflect.DeepEqual(got, []string{"https://example.test/four"}) {
		t.Fatalf("fetchURLs() = %v, want one result from page 4", got)
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
