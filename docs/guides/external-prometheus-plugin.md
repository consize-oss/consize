# Standalone Prometheus plugin

The [Consize Prometheus plugin](https://github.com/consize-oss/consize-prometheus-plugin)
is independently versioned and distributed by the `consize-oss` organization.
Its repository contains source, configuration documentation, Linux/macOS binaries,
and an Ed25519-signed catalog for the existing protocol 1 installer.

This requires the v0.3 development runtime. Consize v0.2.0 does not support it.
The initial plugin release is tested against core revision
[`e196f1e16092`](https://github.com/consize-oss/consize/commit/e196f1e160920f7cdec394665d051251311bad08).
Check the plugin's compatibility notes before using a different core revision.

## Discover and install

- [Release downloads](https://github.com/consize-oss/consize-prometheus-plugin/releases)
- [Signed catalog](https://github.com/consize-oss/consize-prometheus-plugin/releases/latest/download/catalog.json)
- [Integration metadata](https://github.com/consize-oss/consize-prometheus-plugin/blob/main/plugin.json)
- [Installation and activation instructions](https://github.com/consize-oss/consize-prometheus-plugin#install)

The plugin README documents the publisher trust key, exact-version installation,
configuration, and telemetry ownership requirements. Installation verifies the
signed catalog and executable digest; activation requires explicit
`external_plugins` configuration. Disable the built-in Prometheus plugin when
using this external plugin, since both use ID `prometheus-metrics`.

The core UI marketplace is not available yet. These repository and catalog links
provide explicit discovery and installation; they do not imply a global catalog
or automatic trust in publishers.
