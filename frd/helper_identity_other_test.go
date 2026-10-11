//go:build !darwin && !linux && !windows

package frd

import "testing"

// identity is zero here: without a native listing, entries carry no change
// time or file id.
func identity(*testing.T, string) (changeNanos int64, fileID uint64) { return 0, 0 }
