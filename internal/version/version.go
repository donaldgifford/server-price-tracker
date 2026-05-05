// Package version exposes build-time identifiers (commit SHA, semver) so
// runtime code can attach them to telemetry resource attributes and log
// fields. Values are overridden at link time via -ldflags; defaults are
// safe placeholders for `go run` development.
package version

// CommitSHA is the git commit hash the binary was built from. Overridden
// at link time via:
//
//	-ldflags "-X github.com/donaldgifford/server-price-tracker/internal/version.CommitSHA=$(git rev-parse HEAD)"
//
// Defaults to "dev" when unset (e.g., `go run`).
var CommitSHA = "dev"

// Semver is the semantic version of the build (e.g., "v0.8.1"). Overridden
// at link time the same way; defaults to "dev".
var Semver = "dev"
