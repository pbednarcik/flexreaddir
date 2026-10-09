//go:build !amd64 || purego

package frd

import "testing"

// forEachTier runs f once: here the Go loop is the only tier.
func forEachTier(t *testing.T, f func(tier string)) { f("go") }
