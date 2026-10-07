//go:build !darwin && !linux

package frd

import "testing"

// identity is zero here: these arms list no change time and no file id.
func identity(*testing.T, string) (changeNanos int64, fileID uint64) { return 0, 0 }
