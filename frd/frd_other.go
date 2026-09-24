//go:build !darwin && !linux

package frd

import (
	"errors"
	"io/fs"
	"os"
)

func appendDir(dst []Entry, dir *os.File) ([]Entry, error) {
	read, readErr := dir.ReadDir(-1)
	for _, de := range read {
		e := Entry{Name: de.Name(), Type: de.Type()}
		if e.Type.IsRegular() {
			info, err := de.Info()
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return dst, err
			}
			e.Size, e.ModTimeNanos = info.Size(), info.ModTime().UnixNano()
		}
		dst = append(dst, e)
	}
	return dst, readErr
}
