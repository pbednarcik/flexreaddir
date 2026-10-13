// Package frd, FlexReadDir, lists a directory together with the size and
// modification time of every regular file in it, which is what a tree walker
// needs to decide what changed. On macOS, Linux and Windows a listing costs a
// handful of allocations, not one per entry.
//
// On macOS one getattrlistbulk(2) call returns the names, types and
// attributes of many entries at once, where readdir plus a stat per file costs
// one system call per file. The call is made without cgo, through libSystem,
// the way golang.org/x/sys/unix calls libc. A [Lister] with Names set lists
// names and types alone by getdirentries64, which reads the directory's
// records and nothing else, unless a record carries no type, which APFS
// never returns; it is libSystem's private __getdirentries64, linked by
// every macOS binary that imports this package.
//
// On Linux the directory is read as raw getdents64 records and each regular
// file is stat'ed with statx relative to the directory, its name passed in
// place, where fs.DirEntry.Info resolves the whole path again and copies it.
//
// On Windows the directory is read through GetFileInformationByHandleEx as
// FILE_ID_EXTD_DIR_INFO records (FILE_FULL_DIR_INFO where a volume refuses
// that class), which already carry sizes, times and the file id. What the
// package removes is the standard library's allocations per entry.
//
// Elsewhere, AppendDir reads the directory with os.File.ReadDir and stats each
// regular file.
package frd
