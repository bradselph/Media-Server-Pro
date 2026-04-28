package follower

import "os"

// mkdirAll is a minimal alias used by the tests in this package so the test
// helpers don't bring in os directly in every file.
func mkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
