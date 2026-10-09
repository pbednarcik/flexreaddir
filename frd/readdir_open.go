//go:build !darwin && !linux

package frd

import "os"

func openDir(name string) (*os.File, error) { return os.Open(name) }
