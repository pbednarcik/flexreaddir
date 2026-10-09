// Package frd, FlexReadDir, lists a directory together with the size and
// modification time of every regular file in it, which is what a tree walker
// needs to decide what changed. On macOS, Linux and Windows a listing costs a
// handful of allocations, not one per entry.
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
// On Windows the directory is read as FILE_FULL_DIR_INFO records through
// GetFileInformationByHandleEx, which carry sizes and times, so the cost the
// package removes is the standard library's per-entry FileInfo, name and
// DirEntry allocations. The FILE_ID_EXTD_DIR_INFO class would add the file
// id at 5 to 9% of a warm listing, so FileID stays zero on Windows.
//
// Elsewhere AppendDir reads the directory with os.File.ReadDir and stats each
// regular file.
package frd
