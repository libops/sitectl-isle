// Package islandora provides Islandora-specific functionality on top of the
// generic Drupal API client from sitectl-drupal.
package islandora

import (
	"embed"

	"github.com/libops/sitectl-drupal/drupal"
)

// Embed the Islandora Starter Site bundle definitions.
// These are generated from the Drupal config sync using:
//   go run github.com/libops/sitectl-drupal/scripts/generate-bundles \
//       --config-sync /path/to/islandora-starter-site/config/sync \
//       --output ./pkg/islandora/bundles
//
//go:embed bundles/*.yaml
var bundleFS embed.FS

// NewClient creates a Drupal client pre-loaded with Islandora Starter Site
// bundle definitions. Additional options can override or extend these defaults.
//
// Example:
//
//	client := islandora.NewClient()
//	node, _ := client.FetchNode("https://example.com/node/123?_format=json")
//	errors := node.Validate()  // validates against islandora_object bundle
//
// With custom bundles overlaid:
//
//	client := islandora.NewClient(
//	    drupal.WithBundlesFromPath("/path/to/custom/bundles"),
//	)
func NewClient(opts ...drupal.ClientOption) *drupal.Client {
	// Start with Islandora starter site bundles
	allOpts := []drupal.ClientOption{
		drupal.WithEmbeddedBundles(bundleFS, "bundles"),
	}

	// Add user's options (may override with custom bundles)
	allOpts = append(allOpts, opts...)

	return drupal.NewClient(allOpts...)
}

// Node is an alias for drupal.Node for convenience
type Node = drupal.Node

// Client is an alias for drupal.Client for convenience
type Client = drupal.Client
