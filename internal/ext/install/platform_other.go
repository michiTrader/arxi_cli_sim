//go:build !windows

package install

import "io/fs"

func reparsePoint(fs.FileInfo) bool { return false }
