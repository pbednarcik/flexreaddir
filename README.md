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
- Windows: not measured. FlexReadDir uses the standard library there, because
  Windows already returns sizes and times with the listing.

**Not a fully quiet machine.** Other development work was paused during the
runs, but macOS's own services were not: the one-minute load average, sampled
12 times, stayed between 3.9 and 5.4 on 18 cores. Every row's 95% confidence
interval is within ±5% of its median.

### Walking the Linux kernel source: 95,910 files in 6,282 directories

Sequentially:

| How | macOS | Linux (Docker) |
|---|---:|---:|
| `filepath.Walk` | 274 ms | 107 ms |
| `filepath.WalkDir`, with `d.Info()` for each file | 272 ms | 105 ms |
| `os.File.ReadDir` + `Info`, one directory at a time | 255 ms | 90 ms |
| [fastwalk](https://github.com/charlievieth/fastwalk) v1.0.14 with 1 worker, `d.Info()` for each file | 281 ms | 126 ms |
| **FlexReadDir**, one directory at a time | **169 ms** | **70 ms** |
| Time saved against `filepath.WalkDir` | 38% | 33% |

In parallel, with fastwalk's default worker count for each machine (10 on
this Mac, 18 in the container) given to both:

| How | macOS | Linux (Docker) |
|---|---:|---:|
| fastwalk v1.0.14, `d.Info()` for each file | 86.0 ms | 102.0 ms |
| **FlexReadDir** in a minimal worker pool | **59.3 ms** | **30.7 ms** |
| Time saved against fastwalk | 31% | 70% |

FlexReadDir lists one directory and does not walk trees, so the parallel row
uses the small worker pool in [`bench/tree_test.go`](bench/tree_test.go). fastwalk
is built for parallel walks, so its one-worker row is for reference, not its
intended use. In the container, fastwalk's parallel walk was slower than a
sequential walk with `ReadDir`; we have not found out why, and it may not hold
outside a VM.

`filepath.Walk` and `filepath.WalkDir` also sort every directory, which the
other walks do not. That is part of their cost, and anyone calling them pays
it.

### Listing one directory

| How | 1,000 files, macOS | 1,000 files, Linux (Docker) | 10 files, macOS | 10 files, Linux (Docker) |
|---|---:|---:|---:|---:|
| `os.File.ReadDir` + `Info` | 1,382 µs | 603 µs | 25.3 µs | 7.8 µs |
| `os.File.Readdir` (older API, a `FileInfo` per entry) | 947 µs | 465 µs | 20.6 µs | 6.7 µs |
| **FlexReadDir** | **362 µs** | **376 µs** | **13.6 µs** | **5.8 µs** |
| Time saved against `ReadDir` + `Info` | 74% | 38% | 46% | 25% |

### Memory

The names from one directory read share one allocation instead of one each.
The FlexReadDir rows reuse one result slice across listings, as the API
section shows; the standard library returns a new slice every time.

| How | 1,000 files, macOS | 1,000 files, Linux (Docker) | Kernel source, macOS | Kernel source, Linux (Docker) |
|---|---:|---:|---:|---:|
| Standard library (`ReadDir` + `Info`) | 5,017 allocations, 504 KiB | 5,015 allocations, 426 KiB | 562 k allocations, 57.3 MiB | 549 k allocations, 40.9 MiB |
| fastwalk with 1 worker | | | 691 k allocations, 57.8 MiB | 697 k allocations, 44.8 MiB |
| **FlexReadDir** | **5 allocations, 11.5 KiB** | **4 allocations, 12.2 KiB** | **31 k allocations, 3.3 MiB** | **31 k allocations, 2.6 MiB** |

The kernel-source counts include the walk's own path joins, which every row
pays.

### How it was measured, and what was not

All figures are medians of 10 runs, taken in one session with a warm file
cache. Each run is one `go test -bench -benchmem -count 1` pass over every row,
so a slow spell of the machine is spread over all rows instead of falling on
one row's ten runs. Every FlexReadDir row differs from each row it is compared
with at p = 0.000 in `benchstat`. A second complete set of 10 runs, taken
afterwards, reproduced every median within 2% on macOS and 5% in the
container, with the same allocation counts. To reproduce:

```sh
for i in $(seq 10); do go test ./frd -run '^$' -bench 'AppendDir' -benchmem -count 1; done
cd bench && for i in $(seq 10); do FRD_TREE=/path/to/linux go test -run '^$' -bench . -benchmem -count 1; done
```

Not measured yet: a fully quiet machine, Linux on real hardware, Windows,
Intel machines, a cold file cache, and network filesystems.

Why each platform is faster:

- **macOS**: `getattrlistbulk(2)` returns the names, types, sizes and
  modification times of many entries in one system call, where the portable
  way makes one `lstat` per file.
- **Linux**: the directory is read as raw `getdents64` records and each regular
  file is `statx`'d relative to the directory, with its name passed straight
  from the kernel's buffer. The portable way resolves the full path again and
  copies the name for every file.
- **macOS and Linux**: all the names from one kernel read share a single
  allocation and the result grows once per read, so a listing allocates a
  handful of times whatever its size.
- **Everywhere**: `AppendDir` appends to a slice you can reuse across
  directories.

## API

The whole package is four names.

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

### `Entry`

```go
type Entry struct {
	Name         string      // the name within its directory, without a path
	Type         fs.FileMode // type bits only, as fs.DirEntry.Type reports them
	Size         int64       // a regular file's data length in bytes
	ModTimeNanos int64       // a regular file's modification time, Unix nanoseconds
}
```

One directory entry. `Size` and `ModTimeNanos` are filled for regular files
and zero for every other type, on every platform. The names from one read
share one allocation, so a `Name` kept long after its neighbours are gone is
worth a `strings.Clone`. `Entry` is comparable, so
deciding whether a file changed since the last listing is one `==`:

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

The buffers the kernel fills, macOS attribute records and Linux `getdents64`
records, are parsed in plain, bounds-checked Go on every platform and fuzzed
in CI: a buffer that does not hold what it claims is `ErrMalformed`, never a
panic.

## Requirements

Go 1.26 or later.

## License

MIT, see [LICENSE](LICENSE).
