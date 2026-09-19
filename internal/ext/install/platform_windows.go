//go:build windows

package install

import (
	"io/fs"
	"syscall"
)

const fileAttributeReparsePoint = 0x400

func reparsePoint(info fs.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&fileAttributeReparsePoint != 0
}
