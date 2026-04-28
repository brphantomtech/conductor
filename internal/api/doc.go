// Package api is the HTTP and WebSocket surface (SPEC §18). It hosts the
// Chi-routed REST endpoints under /api/v1, the Gorilla WebSocket hub on
// /ws, and serves the embedded SvelteKit dashboard at /. Optional auth
// (bearer token, OIDC) is configured here.
//
// Tier 5 (surface). Imports Tier 0 through Tier 4. The web/ frontend is
// consumed by this package via the Go embed package — web/ itself is not
// a Go package.
package api
