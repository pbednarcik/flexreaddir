# FlexReadDir

[![Go Reference](https://pkg.go.dev/badge/github.com/pbednarcik/flexreaddir/frd.svg)](https://pkg.go.dev/github.com/pbednarcik/flexreaddir/frd)
[![CI](https://github.com/pbednarcik/flexreaddir/actions/workflows/ci.yml/badge.svg)](https://github.com/pbednarcik/flexreaddir/actions/workflows/ci.yml)

List a directory with the size and modification time of every regular file,
in one pass, as fast as each platform allows. No cgo on any platform.

```sh
go get github.com/pbednarcik/flexreaddir/frd
```

```go
import "github.com/pbednarcik/flexreaddir/frd"
```

## Speed

FlexReadDir is compared with what Go programs use today to get the same
answer: every name and type, plus the size and modification time of every
regular file. Times are how long the whole job takes; lower is better.

**Machines**

- macOS: Apple M5 Max (18 cores: 6 super, 12 performance), 128 GB, macOS 27.0,
  internal SSD with APFS, Go 1.27.1. Measured natively.
- Linux: **not bare metal.** A Docker container (`golang:1.27`, arm64,
  Go 1.27.1, 18 CPUs) in OrbStack's Linux VM (kernel 7.0) on the same Mac,
  reading a Docker volume (btrfs) inside the VM, not a folder shared from the
  Mac. A VM adds cost to every system call, so these rows show the direction,
  not the size you would see on a Linux machine.
- Windows: AMD Ryzen 9 9950X (16 cores, 32 threads), 96 GB, Windows 11 Pro
  26200, NTFS on an Intel Optane P5800X with a RAM block cache in front of it,
  Go 1.27.0. Measured natively; the single-directory and sequential rows were
  pinned to one 8-core CCD, the parallel rows were not. The kernel-source rows
  were taken later, on Windows 11 Pro 26300, from a Crucial T700 NVMe drive
  with no block cache in front of it. Every Windows timing is warm-cache, so
  it measures the code, not the disk.

**Not a fully quiet machine.** Other development work was paused during the
runs, but macOS's own services were not: the one-minute load average, sampled
12 times, stayed between 3.9 and 5.4 on 18 cores. Every row's 95% confidence
interval is within ±5% of its median.

### Walking the Linux kernel source: 95,910 files in 6,282 directories

The Windows column walked a newer checkout of the same tree, 96,061 files in
6,297 directories.

Sequentially:

| How | macOS | Linux (Docker) | Windows |
|---|---:|---:|---:|
| `filepath.Walk` | 229 ms | 97 ms | 743 ms |
| `filepath.WalkDir`, with `d.Info()` for each file | 224 ms | 95 ms | 157 ms |
| `os.File.ReadDir` + `Info`, one directory at a time | 212 ms | 81 ms | 128 ms |
| [fastwalk](https://github.com/charlievieth/fastwalk) v1.0.14 with 1 worker, `d.Info()` for each file | 238 ms | 123 ms | 158 ms |
| [godirwalk](https://github.com/karrick/godirwalk) v1.17.0, unsorted, `os.Lstat` for each file | 378 ms | 96 ms | cannot walk |
| **FlexReadDir**, one directory at a time | **134 ms** | **59 ms** | **111 ms** |
| Time saved against `filepath.WalkDir` | 40% | 38% | 29% |

On Windows `filepath.Walk` stands out: it makes a separate `os.Lstat` call by
full path for every file, where the other walks take what the directory
listing already returned.

In parallel, with fastwalk's default worker count for each machine (10 on
this Mac, 18 in the container, 32 on the Windows machine) given to both:

| How | macOS | Linux (Docker) | Windows |
|---|---:|---:|---:|
| fastwalk v1.0.14, `d.Info()` for each file | 85.8 ms | 99.1 ms | 71.7 ms |
| [saracen/walker](https://github.com/saracen/walker) v0.1.4, its `os.FileInfo` | 164 ms | 66.6 ms | 53.4 ms |
| **FlexReadDir** in a minimal worker pool | **57.9 ms** | **26.0 ms** | **28.1 ms** |
| Time saved against fastwalk | 33% | 74% | 61% |

Against saracen/walker FlexReadDir saves 65% on macOS, with 7.4 MiB and 32 k
allocations per walk against its 50.1 MiB and 534 k (fastwalk: 57.9 MiB,
691 k); 61% in the container, with 4.6 MiB and 32 k against its 35.9 MiB and
520 k (fastwalk: 44.8 MiB, 697 k); and 47% on Windows, with 13.7 MiB and 36 k
against its 54.0 MiB and 365 k (fastwalk: 63.6 MiB, 462 k). On macOS
saracen/walker is slower than fastwalk; in the container and on Windows it is
faster. Each platform's rows come from one run.

godirwalk reports no sizes or times, so its row carries an `os.Lstat` per
regular file, which any caller needing them would add. On macOS that makes it
the slowest walk, at 150 MiB against the others' 53 to 66; in the container it
matches `filepath.Walk`. On Windows v1.17.0 cannot walk at all: its scanner
reads with `os.File.Readdir(1)` and takes the `io.EOF` that ends every
directory for an error.

FlexReadDir lists one directory and does not walk trees, so the parallel row
uses the small worker pool in [`bench/tree_test.go`](bench/tree_test.go). fastwalk
is built for parallel walks, so its one-worker row is for reference, not its
intended use. In the container, fastwalk's parallel walk was slower than a
sequential walk with `ReadDir`; we have not found out why, and it may not hold
outside a VM.

`filepath.Walk` and `filepath.WalkDir` also sort every directory, which the
other walks do not. That is part of their cost, and anyone calling them pays
it.

### Walking `C:\Windows\WinSxS`: 105,402 files in 170,544 directories

A Windows system tree, the opposite shape to the kernel source: more
directories than files, and names averaging 94 characters. Measured on the
Windows machine above, ten runs, warm cache.

| How | Sequential | In parallel (32 workers) |
|---|---:|---:|
| `filepath.Walk` | 10.98 s | |
| `filepath.WalkDir`, with `d.Info()` for each file | 6.59 s | |
| `os.File.ReadDir` + `Info`, one directory at a time | 6.05 s | |
| fastwalk v1.0.14, `d.Info()` for each file | 6.63 s (1 worker) | 1.93 s |
| **FlexReadDir** | **5.70 s** | **1.91 s** |
| Time saved | 13% against `filepath.WalkDir`, 6% against `ReadDir` | 1% against fastwalk, within noise (p=0.63) |

Memory for the sequential walk: `ReadDir` + `Info` 1.67 M allocations and
196 MiB, fastwalk 1.89 M and 209 MiB, FlexReadDir 929 k and 150 MiB; the
counts include the walk's own path joins and directory opens. On this tree
each directory costs the walk about 33 µs on one core, and most of that is
opening the directory handle, which the walker does before FlexReadDir is
called. A walker that opened each child relative to its parent's handle would
gain more here than any listing library can.

### Listing one directory

| How | 1,000 files, macOS | 1,000 files, Linux (Docker) | 1,000 files, Windows | 10 files, macOS | 10 files, Linux (Docker) | 10 files, Windows |
|---|---:|---:|---:|---:|---:|---:|
| `os.File.ReadDir` + `Info` | 1,382 µs | 603 µs | 190.0 µs | 25.3 µs | 7.8 µs | 15.7 µs |
| `os.File.Readdir` (older API, a `FileInfo` per entry) | 947 µs | 465 µs | 187.8 µs | 20.6 µs | 6.7 µs | 16.4 µs |
| **FlexReadDir** | **362 µs** | **376 µs** | **139.1 µs** | **13.6 µs** | **5.8 µs** | **13.8 µs** |
| Time saved against `ReadDir` + `Info` | 74% | 38% | 27% | 46% | 25% | 12% |

Windows already returns sizes and times with the listing, so there is no
per-file system call to remove there; the saving is the per-entry work the
standard library does in user space.

### Memory

The names from one directory read share one allocation instead of one each.
The FlexReadDir rows reuse one result slice across listings, as the API
section shows; the standard library returns a new slice every time.

| How | 1,000 files, macOS | 1,000 files, Linux (Docker) | 1,000 files, Windows | Kernel source, macOS | Kernel source, Linux (Docker) | Kernel source, Windows |
|---|---:|---:|---:|---:|---:|---:|
| Standard library (`ReadDir` + `Info`) | 5,017 allocations, 504 KiB | 5,015 allocations, 426 KiB | 2,015 allocations, 160 KiB | 562 k allocations, 57.3 MiB | 549 k allocations, 40.9 MiB | 265 k allocations, 19.9 MiB |
| fastwalk with 1 worker | | | | 691 k allocations, 57.8 MiB | 697 k allocations, 44.8 MiB | 460 k allocations, 31.2 MiB |
| **FlexReadDir** | **5 allocations, 11.5 KiB** | **4 allocations, 12.2 KiB** | **5 allocations, 12.4 KiB** | **31 k allocations, 3.2 MiB** | **31 k allocations, 2.6 MiB** | **35 k allocations, 4.0 MiB** |

The kernel-source counts include the walk's own path joins, which every row
pays.

### How it was measured, and what was not

All figures are medians of 10 runs, taken in one session with a warm file
cache. Each run is one `go test -bench -benchmem -count 1` pass over every row,
so a slow spell of the machine is spread over all rows instead of falling on
one row's ten runs. Every FlexReadDir row differs from each row it is compared
with at p = 0.000 in `benchstat`. A second complete set of 10 runs, taken
afterwards, reproduced every median within 2% on macOS and 5% in the
container, with the same allocation counts.

The macOS kernel-source rows were retaken on 2026-10-08 at v0.3.0, with
godirwalk and saracen/walker in the same run: ten passes of a prebuilt test
binary, the one-minute load average between 3.7 and 6.7 with the benchmark's
own parallel rows counted in it. The intervals of the two `filepath` rows and
the parallel FlexReadDir row ran to ±13%, the rest within ±6%; a second
complete set reproduced every median within 3%. The Linux rows were retaken
the same day in the container, the same way, load 3.1 to 7.3: the three
parallel rows' intervals ran to ±9%, the rest within ±5%, and a second set
reproduced every median within 5%.

The Windows rows were taken the same way, from a prebuilt test binary behind
a quiet gate (total CPU under 8% for six 5-second samples; the last sample
read 2.9%) and pinned to one CCD, so `GOMAXPROCS` was 16. Every row's 95%
interval is within ±5% of its median. The second set reproduced every median
within 4% with the same allocation counts, though its 1,000-file intervals
were wider, up to ±9%. To reproduce:

```sh
for i in $(seq 10); do go test ./frd -run '^$' -bench 'AppendDir' -benchmem -count 1; done
cd bench && for i in $(seq 10); do FRD_TREE=/path/to/linux go test -run '^$' -bench . -benchmem -count 1; done
```

```powershell
1..10 | ForEach-Object { go test ./frd -run '^$' -bench 'AppendDir' -benchmem -count 1 }
```

```powershell
$env:FRD_TREE = 'D:\path\to\linux'; Set-Location bench
1..10 | ForEach-Object { go test -run '^$' -bench . -benchmem -count 1 }
```

The Windows kernel-source rows were taken the same way on 2026-10-07: quiet
gate (last sample 6.1%), ten passes, sequential rows pinned, parallel rows
not, every FlexReadDir row different from its comparison at p = 0.000.

Not measured yet: a fully quiet machine, Linux on real hardware, Intel
machines, a cold file cache, and network filesystems. Every tree row above is
warm-cache, so it shows what the code costs, not what a laptop's disk would
add.

Why each platform is faster:

- **macOS**: `getattrlistbulk(2)` returns the names, types, sizes and
  modification times of many entries in one system call, where the portable
  way makes one `lstat` per file.
- **Linux**: the directory is read as raw `getdents64` records and each regular
  file is `statx`'d relative to the directory, with its name passed straight
  from the kernel's buffer. The portable way resolves the full path again and
  copies the name for every file.
- **Windows**: the directory is read as `FILE_FULL_DIR_INFO` records, which already
  carry sizes and times; the standard library builds a `FileInfo`, a
  name and a `DirEntry` for each, FlexReadDir one `Entry` with its name in a
  shared allocation. A junction is `fs.ModeIrregular`, as `os.ReadDir` reports
  it since Go 1.23; the pre-1.23 `winsymlink=0` setting is not honoured. Names are
  decoded from UTF-16 thirty-two units at a time with AVX2, sixteen with SSE2 on any
  other amd64, and one at a time in Go elsewhere and with `-tags purego`; all three are
  tested against each other. Measured on a Ryzen 9 9950X, the kernels decode WinSxS's
  long names four to five times faster than the Go loop and take 17 to 20% off a warm
  listing of that directory, 6 to 7% off System32's; short names (eleven units) stay on
  the Go loop and cost about a fifth more in the kernel build
  (`docs/superpowers/evidence/2026-10-09-simd-decoder.md`).
- **macOS, Linux and Windows**: all the names from one kernel read share a
  single allocation and the result grows once per read, so a listing allocates
  a handful of times whatever its size.
- **Everywhere**: `AppendDir` appends to a slice you can reuse across
  directories.

## API

The whole package is seven names. Runnable examples for each, including a tree
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
failure is an `*os.PathError`, returned with the entries read before it.

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
its finalizer closes the handle. Where there is no native
listing it returns an error wrapping `errors.ErrUnsupported`.

On the Windows machine above, a parallel walk of the kernel source with one
kept `Lister` per worker and `AppendHandle` allocated 3.7 MiB against 19.6 MiB
with the function `AppendDir`; on the Mac, 3.4 MiB against 8.6 MiB. The bytes
the pool loses are buffers dropped at collections, so a walk with the
collector off allocates the kept figure on both machines. Whether that costs
time depends on the platform. On Windows every collection makes the walk's
threads queue on the Go scheduler's lock as they enter system calls, and more
than half the wall time went there
([the rows](docs/superpowers/evidence/2026-10-07-lister.md)). On macOS the
same walk runs at the same speed with the collector on or off, within 2.5%:
there it is the kernel's time, and `AppendHandle` saves only the `os.File` per
directory ([the rows](docs/superpowers/evidence/2026-10-08-lister-macos.md)).

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
change time. `FileID` comes from `ATTR_CMN_FILEID` on macOS and `stx_ino` on Linux,
and is zero on Windows: the `FILE_ID_EXTD_DIR_INFO` class that carries a 64-bit id on
NTFS costs a warm listing 5 to 9% (measured,
`docs/superpowers/evidence/2026-10-09-fileid-windows.md`), so its parser and fallback
are in the tree but not the default. Zero means unknown, never equal. With
both present, a walker can tell a file replaced by another from one rewritten
in place.

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
the zero Unix time for anything but a regular file.

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
`//go:cgo_import_dynamic` directive, a one-instruction assembly trampoline,
and the runtime's `syscall.syscall6`, which Go keeps reachable by linkname
for exactly this kind of package. If a future Go release closed that
door, the package would stop building on macOS rather than misbehave, and
`golang.org/x/sys` would be in the same position.

The buffers the kernel fills, macOS attribute records, Linux `getdents64`
records and Windows `FILE_FULL_DIR_INFO` (and `FILE_ID_EXTD_DIR_INFO`) records, are walked in plain,
bounds-checked Go on every platform and fuzzed in CI: a buffer that does not
hold what it claims is `ErrMalformed`, never a panic. The one piece of assembly decodes
Windows names on amd64, inside a name the Go walk has already bounded; it is fuzzed against
the Go decoder, which `-tags purego` selects. Windows needs no
trampoline: `GetFileInformationByHandleEx` comes from `golang.org/x/sys/windows`.

## Status

v0: the seven names are meant to stay, but the module is young and the API is
not frozen until v1. Every platform path is tested and fuzzed in CI on Linux,
macOS and Windows, on the current and previous Go release.

Design notes, plans and the measurements behind every number in this file are
under [`docs/superpowers`](docs/superpowers), one dated file per question.

## Requirements

Go 1.26 or later.

## License

MIT, see [LICENSE](LICENSE).
