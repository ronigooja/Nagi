//go:build !darwin

package installtx

import "os"

func fullSync(f *os.File) error { return f.Sync() }
