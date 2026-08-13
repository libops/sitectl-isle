package endpoint

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
)

func TestProviderResolvesNamedISLEEndpoints(t *testing.T) {
	t.Parallel()

	ctx := &config.Context{
		DockerHostType: config.ContextLocal,
		ProjectDir:     ".",
		ComposeFile:    []string{filepath.Join("testdata", "all-services.yaml")},
	}
	provider := Provider{Defaults: func(*config.Context) Defaults {
		return Defaults{Scheme: "https", Domain: "repo.example.org"}
	}}
	tests := []struct {
		name string
		call func() (Resolved, error)
		want string
	}{
		{name: AppRoute, call: func() (Resolved, error) { return provider.App(nil, ctx) }, want: "https://repo.example.org"},
		{name: FCRepoRoute, call: func() (Resolved, error) { return provider.FCRepo(nil, ctx) }, want: "https://fcrepo.repo.example.org"},
		{name: IIIFRoute, call: func() (Resolved, error) { return provider.IIIF(nil, ctx) }, want: "https://repo.example.org/iiif"},
		{name: CantaloupeRoute, call: func() (Resolved, error) { return provider.Cantaloupe(nil, ctx) }, want: "https://repo.example.org/cantaloupe"},
		{name: BlazegraphRoute, call: func() (Resolved, error) { return provider.Blazegraph(nil, ctx) }, want: "https://blazegraph.repo.example.org"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.call()
			if err != nil {
				t.Fatalf("resolve %s endpoint error = %v", tt.name, err)
			}
			if got.Route.Name != tt.name || got.URL != tt.want {
				t.Fatalf("resolve %s endpoint = %+v, want URL %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestProviderOmitsUnavailableOptionalEndpoint(t *testing.T) {
	t.Parallel()

	provider := Provider{}
	_, err := provider.FCRepo(nil, nil)
	if !errors.Is(err, ErrRouteNotFound) {
		t.Fatalf("FCRepo() error = %v, want ErrRouteNotFound", err)
	}
}

func TestProviderDoesNotDeclareRemoteLocalhost(t *testing.T) {
	t.Parallel()

	ctx := &config.Context{DockerHostType: config.ContextRemote}
	if got := defaultDomain(ctx); got != "" {
		t.Fatalf("defaultDomain(remote) = %q, want empty", got)
	}
	resolved, err := plugin.ResolveIngressRoute(ctx, plugin.IngressRoutes{Routes: []plugin.IngressRoute{{
		Name:          AppRoute,
		DefaultScheme: "http",
	}}}, AppRoute)
	if !errors.Is(err, ErrRouteURLUnresolved) {
		t.Fatalf("ResolveIngressRoute() error = %v, want ErrRouteURLUnresolved", err)
	}
	if resolved.URL != "" {
		t.Fatalf("ResolveIngressRoute() URL = %q, want no implicit remote localhost", resolved.URL)
	}
}
