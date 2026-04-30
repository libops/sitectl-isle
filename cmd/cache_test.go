package cmd

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestFetchURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"url":"https://example.test/one"},
			{"url":""},
			{"url":"https://example.test/two"}
		]`))
	}))
	defer server.Close()

	got, err := fetchURLs(context.Background(), server.URL)
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

	_, err := fetchURLs(context.Background(), server.URL)
	if err == nil {
		t.Fatal("fetchURLs() error = nil, want status error")
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
