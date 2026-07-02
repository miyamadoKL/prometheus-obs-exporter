# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [Unreleased]

Full rewrite: this project is forked from `paychex/prometheus-emcecs-exporter`
and rebuilt to support Dell ObjectScale 4.1.x and Dell EMC ECS 3.8.1.x in a
single binary. See `docs/design.md` for the complete design.

### Breaking Changes
- Module renamed from `github.com/paychex/prometheus-emcecs-exporter` to
  `github.com/miyamadoKL/prometheus-obs-exporter`; binary renamed to
  `obs-exporter`.
- Metric name prefix changed from `emcecs_` to `obs_`; metric names, types,
  and label sets were reworked across all collectors (see README for the
  full mapping). Dashboards/alerts built on the old metric names must be
  updated.
- Scrape endpoint changed from `/query` to `/probe`.
- Flag and environment variable scheme replaced: flags now use
  `--ecs.*` / `--collector.*` / `--web.*` namespaces, and environment
  variables use the `OBS_EXPORTER_*` prefix instead of `ECSENV_*`.
- The `metering` collector is no longer enabled by default and must be
  explicitly requested via `collectors=metering` on `/probe` (previously a
  `metering=1` boolean toggle enabled it alongside the default collectors).
- TLS verification against ECS/ObjectScale is now enabled by default;
  previously it was always skipped. Use `--ecs.tls.insecure-skip-verify` or
  `--ecs.tls.ca-file` to restore trust for self-signed certificates.
- CI moved from Travis CI to GitHub Actions; `.goreleaser.yml` rewritten
  for GoReleaser v2.

### Added
- ObjectScale 4.1.x support alongside ECS 3.8.1.x, in the same binary.
- `--collector.perf.range` is now validated at startup and rejected if it
  exceeds 1h (the Flux API's documented range cap).
- `perf` collector backed by the Flux API, replacing the transaction
  metrics removed from the dashboard API in ECS 3.6.0.0.
- `--ecs.mgmt-port` is now honored by the metering API too (previously
  hardcoded).
- `--collector.node.dt-stats` to disable the DT statistics/ping collector
  on platforms where `:9101` is unavailable.
- `--ecs.tls.ca-file` to trust a specific CA for self-signed certificates.
- `--web.config.file` support (via `prometheus/exporter-toolkit`) for
  TLS/Basic-auth on the exporter's own listener.
- GitHub Actions CI (`.github/workflows/ci.yml`) running build, test, and
  `golangci-lint` on every push and pull request.
- GitHub Actions release workflow (`.github/workflows/release.yml`) running
  GoReleaser v2 on `v*` tag pushes.
- `.golangci.yml` with a standard linter set (govet, staticcheck, errcheck,
  ineffassign, unused, gofmt, goimports).
- `Dockerfile` for building a distroless, non-root `obs-exporter` container
  image.
- `compose.yaml` for running the exporter via `docker compose`, including a
  Japanese "接続設定" section in the README covering credentials, TLS, and
  troubleshooting.

### Removed
- `.travis.yml` and Travis CI integration.
- gjson-based dynamic JSON parsing, logrus logging, and envy-based
  environment variable handling, replaced by typed structs, `log/slog`
  (`prometheus/common/promslog`), and `kingpin`'s built-in `Envar` support.

### Changed
- Node collector metrics renamed: `obs_node_dt_total` / `obs_node_dt_unready`
  / `obs_node_dt_unknown` → `obs_node_directory_tables` /
  `obs_node_directory_tables_unready` / `obs_node_directory_tables_unknown`
  (DT = directory table; a Gauge named `_total` violated Prometheus naming
  conventions).
- `obs_metering_namespace_quota_bytes` is now only emitted when the quota
  API's `blockSize` is greater than 0 (ECS's minimum configurable quota is
  1GB, so `0` means "unset", not an actual zero-byte quota).
- Collector configuration (`--collector.node.dt-stats`,
  `--collector.metering.concurrency`, `--collector.perf.range`) is now
  threaded through as a request-scoped `collector.Settings` value instead of
  package-level variables, and is fully wired from `cfg` into every
  `/probe` call.
- On shutdown, the exporter now drains in-flight `/probe` requests
  (`srv.Shutdown`) before logging out of cached targets, instead of the
  other way around.

## [1.0.0] - 2018-05-17
Initial release - [Mark DeNeve](https://github.com/xphyr)

## [1.1.0] - 2018-09-24
Changes to authentication system to cut down on login/logouts that occur - [Mark DeNeve](https://github.com/xphyr)

## [1.2.0] - 2019-07-13
Updates to project layout, and enhancement to http client usage to cut down on memory usage.
Also changed to use go modules by default and have removed all vendored dependencies
Node info is now gathered over port 9021 to enable SSL. If your ECS arrays are behind a firewall be sure to update your rules to allow port 9021 instead of 9020
Loging has been updated to only use Logrus and time format has been updated to be human readable.
[Mark DeNeve](https://github.com/xphyr)
