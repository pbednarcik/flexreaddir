package frd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// A directory that may be read but not searched lists its names and refuses
// every stat, so the error names the entry and the call that failed.
func TestAnEntryThatCannotBeStatedIsAPathErrorNamingIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	refuseSearch(t, dir)
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = AppendDir(nil, f)
	pe, ok := errors.AsType[*os.PathError](err)
	if !ok || pe.Op != "statx" || pe.Path != filepath.Join(dir, "a.go") || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("AppendDir = %v, want a statx permission error naming a.go", err)
	}
}

// A kernel older than 4.11 answers statx with ENOSYS, and the listing then
// stats through fstatat, which must fill every field statx does.
func TestTheFstatatFallbackListsWhatStatxLists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := readDir(t, dir)["a.go"]
	noStatx.Store(true)
	t.Cleanup(func() { noStatx.Store(false) })
	if got := readDir(t, dir)["a.go"]; got != want {
		t.Fatalf("through fstatat %+v, through statx %+v", got, want)
	}
}

// statx mounts an automount point it is asked about unless told not to;
// fstatat, the fallback, never does, since the kernel adds AT_NO_AUTOMOUNT to
// it, and neither does os.Lstat. Listing a directory must not mount what is
// in it. The flag is pinned here because no test machine has autofs to show
// the mount.
func TestStatxNeverTriggersAnAutomount(t *testing.T) {
	if statxFlags&unix.AT_NO_AUTOMOUNT == 0 {
		t.Fatalf("statx flags %#x lack AT_NO_AUTOMOUNT, which fstatat always has", statxFlags)
	}
}

// A seccomp filter may refuse statx with EPERM rather than ENOSYS (Docker's
// default profile did before 18.04; a hardened container profile still may),
// and the listing must then describe every file through fstatat. A filter
// cannot be lifted once installed, so the test runs itself again as a child
// process, FRD_STATX_EPERM=1, and installs it there.
func TestAListingSurvivesStatxRefusedWithEPERM(t *testing.T) {
	if os.Getenv("FRD_STATX_EPERM") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAListingSurvivesStatxRefusedWithEPERM$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), "FRD_STATX_EPERM=1")
		out, err := cmd.CombinedOutput()
		if err != nil || !bytes.Contains(out, []byte("--- PASS: TestAListingSurvivesStatxRefusedWithEPERM")) {
			t.Fatalf("under a filter refusing statx: %v\n%s", err, out)
		}
		return
	}
	refuseStatx(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.go")
	if err := os.WriteFile(file, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // the call that meets EPERM, then the calls after it
		got := readDir(t, dir)["a.go"]
		if !got.Type.IsRegular() || got.Size != info.Size() || got.ModTimeNanos != info.ModTime().UnixNano() {
			t.Fatalf("listed %+v, lstat says size %d, mtime %d", got, info.Size(), info.ModTime().UnixNano())
		}
	}
}

// refuseStatx installs, on every thread of this process, a seccomp filter
// that answers statx with EPERM and lets every other call through, then
// proves it is on. The filter reads only the call number, not the
// architecture, which is enough for a test binary of the machine's own.
func refuseStatx(t *testing.T) {
	prog := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}, // seccomp_data.nr
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 0, Jf: 1, K: unix.SYS_STATX},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	fprog := unix.SockFprog{Len: uint16(len(prog)), Filter: &prog[0]}
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&fprog)))
	runtime.KeepAlive(prog)
	if errno != 0 {
		t.Fatalf("seccomp: %v", errno)
	}
	var stx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, ".", 0, statxMask, &stx); err != unix.EPERM {
		t.Fatalf("statx answered %v, want EPERM: the filter is not in place", err)
	}
}

// idsPersist is true: statx's stx_ino is the kernel's inode number, which
// lstat reports too.
func idsPersist(*testing.T, string) bool { return true }
