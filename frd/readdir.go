package frd

// ReadDir reads the directory name and returns its entries sorted by name,
// as os.ReadDir does: "." and ".." are left out, links are not followed, and
// an empty directory gives an empty, non-nil slice.
//
// If reading fails part way, ReadDir returns the entries read before the
// error, sorted, with the error. A name that is not a directory gives
// syscall.ENOTDIR, as with os.ReadDir. The function AppendDir is the
// unsorted, reusable form.
func ReadDir(name string) ([]Entry, error) {
	entries, err := listDir(name)
	SortByName(entries)
	return entries, err
}
