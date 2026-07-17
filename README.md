# sitectl-isle

`sitectl-isle` simplifies the creation and operation of repositories created using the [LibOps ISLE template](https://github.com/libops/isle). It uses sitectl components to make features like Traefik bot mitigation, Triplet IIIF, and Islandora filesystem storage easy to enable and customize.

Documentation: https://sitectl.libops.io/plugins/isle

## Requirements

- [`sitectl`](https://sitectl.libops.io/install).
- Docker with the Compose v2 plugin for local ISLE sites.
- [`sitectl-drupal`](https://github.com/libops/sitectl-drupal), because ISLE includes and extends the Drupal plugin surface.

## Quick Start

Create a local ISLE site from the matching template:

```bash
sitectl create isle/default \
  --template-repo https://github.com/libops/isle \
  --path ./my-isle-site \
  --type local \
  --checkout-source template \
  --default-context
```

The template README is at https://github.com/libops/isle.

## Basic Operations

Use [`sitectl compose`](https://sitectl.libops.io/commands/compose) to start or inspect the stack:

```bash
sitectl compose up --remove-orphans -d
```

Use [`sitectl healthcheck`](https://sitectl.libops.io/commands/healthcheck) and [`sitectl validate`](https://sitectl.libops.io/commands/validate) to check the site:

```bash
sitectl healthcheck
sitectl validate
```

Use [`sitectl image`](https://sitectl.libops.io/commands/image) for local image or build-arg overrides:

```bash
sitectl image set --tag drupal=nginx-1.30.3-php84 --tag solr=9
```

Use [`sitectl set`](https://sitectl.libops.io/commands/set) for component changes. It updates the component-owned project files immediately:

```bash
sitectl set bot-mitigation on
```

Feature bundles converge a complete Islandora capability across Compose,
Drupal configuration, and (when needed) `composer.json`:

```bash
# Add the upstream mergepdf service and paged-content action.
sitectl set mergepdf enabled --islandora-tag 6.3.19

# Add hOCR generation, IIIF annotations, Solr indexing, and search.
# Use the term ID for https://discoverygarden.ca/use#hocr on this site.
sitectl set hocr-search enabled --hocr-term-id 56
```

The commands edit source-controlled project files but do not fabricate a
Composer lock file or run a production data backfill. Review the diff, update
`composer.lock` when hOCR changes, rebuild the Drupal image, import Drupal
configuration, and then generate/reindex existing content as the command's
follow-up output directs. `mergepdf` requires `islandora/alpaca:6.3.19` or
newer when the project uses the upstream Alpaca image; hOCR requires
`islandora/solr:4.2.1` or a compatible LibOps Solr image.

Use [`sitectl converge`](https://sitectl.libops.io/commands/converge) later to inspect and repair component drift after manual edits or upstream updates.

See the [ISLE plugin docs](https://sitectl.libops.io/plugins/isle) for Fedora, Blazegraph, IIIF, derivatives, sync, migration, cache, TLS, and bot mitigation details.

## License

`sitectl-isle` is licensed under the MIT License.
