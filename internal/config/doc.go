// Package config is the typed configuration layer for Conductor. It loads
// values from CLI flags, environment variables, HARNESS.md front matter, and
// built-in defaults, in the precedence order described in SPEC §6.1. It is
// the only package permitted to read from os.Getenv and the environment;
// every other package receives its config slice by value at construction.
//
// Tier 0 (foundation). Imports nothing else under internal/.
package config
