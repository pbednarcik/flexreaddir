// Package frd, FlexReadDir, lists a directory together with the size and
// modification time of every regular file in it, which is what a tree walker
// needs to decide what changed. On macOS and Linux a listing costs a handful
// of allocations, not one per entry.
//
// On macOS one getattrlistbulk(2) call returns the names, types and
// attributes of many entries at once, where readdir plus a stat per file costs
// one system call per file. The call is made without cgo, through libSystem,
// the way golang.org/x/sys/unix calls libc.
//
// On Linux the directory is read as raw getdents64 records and each regular
// file is stat'ed with statx relative to the directory, its name passed in
// place, where fs.DirEntry.Info resolves the whole path again and copies it.
//
// Elsewhere AppendDir reads the directory with os.File.ReadDir and stats each
// regular file, which on Windows costs nothing extra: the listing already
// carries sizes and times.
package frd
