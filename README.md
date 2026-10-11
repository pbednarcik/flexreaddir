<div align="center">

<img src=".github/assets/logo.svg" alt="FlexReadDir" width="480">

[![Go Reference](https://pkg.go.dev/badge/github.com/pbednarcik/flexreaddir/frd.svg)](https://pkg.go.dev/github.com/pbednarcik/flexreaddir/frd)
[![CI](https://github.com/pbednarcik/flexreaddir/actions/workflows/ci.yml/badge.svg)](https://github.com/pbednarcik/flexreaddir/actions/workflows/ci.yml)

</div>

List a directory with the size and modification time of every regular file,
in one pass, as fast as each platform allows. No cgo on any platform.

```sh
go get github.com/pbednarcik/flexreaddir/frd
```

```go
import "github.com/pbednarcik/flexreaddir/frd"
```

## Speed

FlexReadDir is compared with what a Go program uses today to get the same
answer from one directory: every name and type, plus the size and
modification time of every regular file. Medians of ten runs, warm cache;
lower is better.

| How | 1,000 files, macOS | 1,000 files, Linux (VM) | 1,000 files, Windows | 10 files, macOS | 10 files, Linux (VM) | 10 files, Windows |
|---|---:|---:|---:|---:|---:|---:|
| `os.File.ReadDir` + `Info` | 1,382 µs | 603 µs | 164.3 µs | 25.3 µs | 7.8 µs | 15.7 µs |
| `os.File.Readdir` (older API, a `FileInfo` per entry) | 947 µs | 465 µs | 161.3 µs | 20.6 µs | 6.7 µs | 15.8 µs |
| **FlexReadDir** | **362 µs** | **376 µs** | **131.1 µs** | **13.6 µs** | **5.8 µs** | **14.1 µs** |
| Time saved against `ReadDir` + `Info` | 74% | 38% | 20% | 46% | 25% | 11% |

Memory for the 1,000-file listing: the standard library makes 5,017
allocations and 504 KiB on macOS, 5,015 and 426 KiB on Linux, 2,015 and
160 KiB on Windows; FlexReadDir makes 5, 4 and 5, about 12 KiB on each,
because the names from one kernel read share one allocation and the result
slice is reused across listings.

Windows already returns sizes and times with the listing, so there is no
per-file system call to remove there; the saving is the per-entry work the
standard library does in user space.

### How close to the floor

What is left is the system call each platform needs. Profiled:

- **macOS**: 98.7% of a 1,000-file listing's CPU samples are inside the
  libSystem call; the parser is 4% of the listing's time.
- **Linux**: the kernel is 91% of a kernel-source walk's CPU, one `statx` per
  regular file 59% of it; FlexReadDir's own code is 4.6% (in a VM).
- **Windows**: NTFS is 84 to 90% of a listing; FlexReadDir's own code was 7 to
  13% before v0.4.0's name decoder cut its parsing by 9 to 14%.

The rows are in [`docs/notes`](docs/notes): `2026-10-10-macos-rows-retaken.md`,
`2026-10-11-linux-profile-docker.md` and `2026-10-11-windows-kernel-trace.md`.

### Machines and method

- **macOS**: Apple M5 Max, 128 GB, macOS 27.0, APFS on the internal SSD, Go
  1.27.1, natively. Taken 2026-09-29; retaken on 2026-10-10 at the code
  v0.4.0 ships, every row within 5%.
- **Linux**: **not bare metal.** A Docker container (arm64, Go 1.27.1) in
  OrbStack's VM on the same Mac, reading a Docker volume, 2026-09-29. A VM
  adds cost to every system call, so these rows show the direction, not the
  size; the Linux parser has changed since and they were not retaken.
- **Windows**: AMD Ryzen 9 9950X, 96 GB, Windows 11 Pro 26300, NTFS, Go
  1.27.0, natively, at v0.4.0 on 2026-10-11: one benchmark per process, the
  order of the arms rotated each round, pinned to one 8-core CCD, total CPU
  0.3 to 6.1% during the runs. Every row's 95% interval is within ±3%.

To reproduce, ten passes of the single-directory benchmark:

```sh
for i in $(seq 10); do go test ./frd -run '^$' -bench 'AppendDir' -benchmem -count 1; done
```

Not measured yet: Linux on real hardware, Intel machines, a cold file cache
and network filesystems.

FlexReadDir lists one directory; it does not walk trees. Walkers are compared
with each other by the walker built on it; the tree walks measured so far, and
what they found about opening directories, worker counts and the garbage
collector, are in [`docs/notes`](docs/notes) and the [`bench`](bench) module.

### Why each platform is faster

- **macOS**: `getattrlistbulk(2)` returns the names, types, sizes and
  modification times of many entries in one system call, where the portable
  way makes one `lstat` per file.
- **Linux**: the directory is read as raw `getdents64` records and each regular
  file is `statx`'d relative to the directory, with its name passed straight
  from the kernel's buffer. The portable way resolves the full path again and
  copies the name for every file.
- **Windows**: the directory is read as `FILE_ID_EXTD_DIR_INFO` records
  (`FILE_FULL_DIR_INFO` where a volume refuses that class: FAT, WSL's 9P, older
  SMB servers), which already carry sizes, times and the file id; the standard
  library builds a `FileInfo`, a name and a `DirEntry` for each, FlexReadDir one
  `Entry` with its name in a shared allocation. Names are decoded from UTF-16
  thirty-two units at a time with AVX2, sixteen with SSE2 on any other amd64,
  and one at a time in Go elsewhere and with `-tags purego`, all three tested
  against each other ([`docs/decoder-kernel.md`](docs/decoder-kernel.md)). A
  junction is `fs.ModeIrregular`, as `os.ReadDir` reports it since Go 1.23.
- **Everywhere**: `AppendDir` appends to a slice you can reuse across
  directories, so a listing allocates a handful of times whatever its size.

## API

The whole package is eight names. Runnable examples for each, including a tree
walk, are in [`frd/example_test.go`](frd/example_test.go) and on
[pkg.go.dev](https://pkg.go.dev/github.com/pbednarcik/flexreaddir/frd#pkg-examples).

### `AppendDir`

```go
func AppendDir(dst []Entry, dir *os.File) ([]Entry, error)
```

Reads the open directory `dir` from its current offset to its end and appends
one `Entry` per name to `dst`. Entries come in no particular order; `"."` and
`".."` are never included; a symbolic link is reported as a link and not
followed; a name removed between the read and its stat is left out. Any other
failure is an `*os.PathError`, returned with the entries read before it and
naming the entry that failed, or the directory when the listing call itself
failed.

Two directories behave differently on macOS. One that may be read but not
searched (`r` without `x`) fails as a whole with a permission error naming the
directory, even when it is empty, where `os.ReadDir` lists its names; on Linux
the listing fails at the first name it has to stat. And in `/dev/fd`, which
holds the caller's own descriptors, `getattrlistbulk` reports every entry as
`fs.ModeIrregular` and stops with `EBADF` at a descriptor `lstat` cannot
describe either, such as the Go runtime's kqueue; `os.ReadDir` follows each
with `lstat` instead.

```go
f, err := os.Open(dir)
if err != nil {
	return err
}
defer f.Close()

entries, err := frd.AppendDir(nil, f)
if err != nil {
	return err
}
for _, e := range entries {
	if e.Type.IsRegular() {
		fmt.Println(e.Name, e.Size)
	}
}
```

Pass the previous result truncated to zero length to reuse its storage across
directories, which keeps a tree walk to a few allocations per directory:

```go
var entries []frd.Entry
for _, dir := range dirs {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	entries, err = frd.AppendDir(entries[:0], f)
	f.Close()
	if err != nil {
		return err
	}
	// use entries before the next iteration overwrites them
}
```

### `Lister`

```go
type Lister struct{ /* the buffer the kernel fills */ }

func (l *Lister) AppendDir(dst []Entry, dir *os.File) ([]Entry, error)
func (l *Lister) AppendHandle(dst []Entry, fd uintptr, name string) ([]Entry, error)
```

The function `AppendDir` borrows the 64 KiB buffer the kernel fills from a
`sync.Pool`, which may drop its buffers at any garbage collection. A walker that
lists many directories keeps a `Lister` per goroutine instead, which owns its
buffer for as long as the walker keeps it. The zero `Lister` is ready to use;
it must not be used by two goroutines at once, nor copied after its first use.
Either way, one directory is listed by one goroutine at a time: two calls on
one `os.File` share its offset and split its entries between them.

`AppendHandle` is for a walker that opens directories itself: it lists a raw
Windows handle or Unix descriptor, the value `os.File.Fd` returns, without
wrapping it in an `os.File`. It reads from the handle's current position and
never closes it; `name` is used only in errors. A handle taken from
`os.File.Fd` needs that `os.File` kept reachable until the call returns, since
its finalizer closes the handle. On platforms other than macOS, Linux and
Windows it returns an error wrapping `errors.ErrUnsupported`.

A kept `Lister` saves the buffers the pool drops at each garbage collection: a
parallel walk of the kernel source allocated 3.7 MiB against 19.6 MiB on
Windows, 3.4 MiB against 8.6 MiB on the Mac. On Windows the collector is also
a parallel walker's largest cost: with `GOGC=400` the same walk ran 43 to 49%
faster ([the rows](docs/notes/evidence/2026-10-10-buffer-and-gc-windows.md)); a
long-lived walker can set it around the walk with `debug.SetGCPercent`. On
macOS the walk is the kernel's time, within 2.5% with the collector on or off
([the rows](docs/notes/evidence/2026-10-08-lister-macos.md)).

### `CompareName` and `SortByName`

```go
func CompareName(a, b Entry) int
func SortByName(entries []Entry)
```

`AppendDir` returns entries in the order the kernel gives them. `SortByName` sorts them
by name in byte order, the order `os.ReadDir` returns, in place and without allocating.
`CompareName` is the same order as a comparison, for `slices.SortFunc`,
`slices.BinarySearchFunc` or a merge.

### `ReadDir`

```go
func ReadDir(name string) ([]Entry, error)
```

Lists the directory `name` and returns its entries sorted by name, as `os.ReadDir` does:
the same order, an `*os.PathError` from the open, `ENOTDIR` for a name that is not a
directory, the entries read before a failure together with the error, and an empty
non-nil slice for an empty directory. It borrows the pooled buffer, as the function
`AppendDir` does; a walker keeps a `Lister` instead.

### `Entry`

```go
type Entry struct {
	Name            string      // the name within its directory, without a path
	Type            fs.FileMode // type bits only, as fs.DirEntry.Type reports them
	Size            int64       // a regular file's data length in bytes
	ModTimeNanos    int64       // a regular file's modification time, Unix nanoseconds
	ChangeTimeNanos int64       // its status change time; macOS, Linux and Windows
	FileID          uint64      // its number on the volume (inode); 0 if unknown
}
```

One directory entry. Every field but `Name` and `Type` is filled for regular
files only and is zero for every other type. `Size` and `ModTimeNanos` are
filled on every platform.

`ChangeTimeNanos` moves on every write and every change to the file's
metadata, such as its permissions. It comes from `ATTR_CMN_CHGTIME` on macOS,
`statx`'s `stx_ctime` on Linux and the listing's own `ChangeTime` on Windows,
and is zero on the other platforms and on a Windows filesystem that keeps no
change time. On macOS a filesystem that keeps none, FAT32 for one, gives the
modification time instead, as `lstat` does. `FileID` comes from
`ATTR_CMN_FILEID` on macOS, `stx_ino` on Linux and the listing's
`FILE_ID_EXTD_DIR_INFO` `FileId` on Windows when it fits 64 bits, as on NTFS;
that class costs a warm listing 1.7 to 3.2% over `FILE_FULL_DIR_INFO` when
last measured (`docs/notes/evidence/2026-10-09-fileid-windows.md`), the price
of the identity. It is zero where the volume gives none: on Windows ReFS and Dev Drive
(128-bit ids), FAT, WSL's 9P and shares on servers older than Windows Server
2025; on macOS a volume that says its ids do not persist, such as FAT32, exFAT
and NFS, where an empty FAT file gets a new id on every listing. Zero means
unknown, never equal. With `ChangeTimeNanos` and `FileID` both present, a
walker can tell a file replaced by another from one rewritten in place.

The names from one read share one allocation, so a `Name` kept long after its
neighbours are gone is worth a `strings.Clone`. `Entry` is comparable, so
deciding whether a file changed since the last listing is one `==`. Because
`==` includes the change time, a permissions change counts as a change on the
platforms that report one:

```go
previous := map[string]frd.Entry{} // from the last listing
for _, e := range entries {
	if e.Type.IsRegular() && previous[e.Name] != e {
		fmt.Println("changed:", e.Name)
	}
}
```

### `Entry.ModTime`

```go
func (e Entry) ModTime() time.Time
```

The modification time as a `time.Time`, for display or date arithmetic. It is
the Unix epoch for anything but a regular file.

```go
for _, e := range entries {
	if e.Type.IsRegular() && time.Since(e.ModTime()) < 24*time.Hour {
		fmt.Println("modified today:", e.Name)
	}
}
```

### `ErrMalformed`

```go
var ErrMalformed = errors.New("frd: malformed directory buffer")
```

Returned when a buffer the kernel filled does not hold the records it claims.
It never describes the directory being read; it means a kernel whose layout
this package does not know, and it is worth a bug report.

```go
entries, err := frd.AppendDir(nil, f)
if errors.Is(err, frd.ErrMalformed) {
	log.Printf("frd does not understand this kernel's directory buffer: %v", err)
}
```

## How the macOS path works without cgo

The package calls libSystem the way `golang.org/x/sys/unix` does: a
`//go:cgo_import_dynamic` directive, a one-instruction assembly trampoline
for each libSystem function, and the runtime's `syscall.syscall6`, which Go keeps reachable by linkname
for exactly this kind of package. If a future Go release closed that
door, the package would stop building on macOS rather than misbehave, and
`golang.org/x/sys` would be in the same position.

The buffers the kernel fills, macOS attribute records, Linux `getdents64`
records and Windows `FILE_ID_EXTD_DIR_INFO` and `FILE_FULL_DIR_INFO` records, are walked in plain,
bounds-checked Go on every platform and fuzzed in CI: a buffer that does not
hold what it claims is `ErrMalformed`, never a panic. The assembly is two small pieces: the
macOS trampoline above, and the amd64 name kernel, which reads at most 62 bytes past a name,
always inside the buffer the Go walk has bounded, and is tested exhaustively and fuzzed against
the Go decoder, which `-tags purego` selects. Windows needs no
trampoline: `GetFileInformationByHandleEx` comes from `golang.org/x/sys/windows`.

## Status

v0: the eight names are meant to stay, but the module is young and the API is
not frozen until v1. CI runs the tests on Linux (amd64 and arm64), macOS
(arm64) and Windows, on Go 1.27, the oldest release it supports, and on the
current release, again with the name decoder's assembly turned off; and it
fuzzes the three record parsers and the name decoder on each of the three
with the current release. The fallback for other systems is vetted, not run.

The measurements behind every number in this file, and the questions they
settled, are under [`docs/notes`](docs/notes), one dated file per question.

## Requirements

Go 1.27 or later.

## License

MIT, see [LICENSE](LICENSE).
