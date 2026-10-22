//go:build darwin

package frd_test

import (
	"fmt"
	"log"
	"os"

	"github.com/pbednarcik/flexreaddir/frd"
)

func ExampleVolume() {
	f, err := os.Open(".")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	conn, err := f.SyscallConn()
	if err != nil {
		log.Fatal(err)
	}
	var caps frd.VolumeCaps
	if cerr := conn.Control(func(fd uintptr) { caps, err = frd.Volume(fd) }); cerr != nil {
		log.Fatal(cerr)
	}
	// The caller folds its own defaults; these are the listing's. A volume
	// that cannot be asked keeps no ids, and a bit the volume does not vouch
	// for is taken as ids kept.
	kept := false
	if err == nil {
		set, known := caps.PersistentIDs()
		kept = !known || set
	}
	fmt.Println(kept)
	// Output: true
}

func ExampleDirLinks() {
	f, err := os.Open(".")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	conn, err := f.SyscallConn()
	if err != nil {
		log.Fatal(err)
	}
	var n uint32
	if cerr := conn.Control(func(fd uintptr) { n, err = frd.DirLinks(fd) }); cerr != nil {
		log.Fatal(cerr)
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(n >= 1)
	// Output: true
}
