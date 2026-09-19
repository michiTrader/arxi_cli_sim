package ext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConsentIdentity is every property to which a persisted grant is bound.
type ConsentIdentity struct {
	Name, Version, Protocol, Executable, PackageDigest string
	Args, Capabilities                                 []string
}

// Identity returns a deterministic consent identity. Capability order is irrelevant.
func Identity(manifest Manifest, packageDigest string) string {
	caps := make([]string, len(manifest.Capabilities))
	for i, capability := range manifest.Capabilities {
		caps[i] = string(capability)
	}
	sort.Strings(caps)
	body, _ := json.Marshal(ConsentIdentity{manifest.Name, manifest.Version, manifest.Protocol, manifest.Executable, packageDigest, append([]string(nil), manifest.Args...), caps})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// TreeDigest hashes a package tree canonically. Each record contains its slash-normalized
// relative path, regular-file type, executable mode bit, byte count, and bytes.
// Directories affect the digest through file paths; symlinks and special entries are rejected.
func TreeDigest(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			if !entry.IsDir() {
				return fmt.Errorf("package root is not a directory")
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("package entry %q is a symlink", path)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package entry %q is not regular", path)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(paths, func(i, j int) bool { return filepath.ToSlash(paths[i]) < filepath.ToSlash(paths[j]) })
	h := sha256.New()
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		exec := "0"
		if info.Mode().Perm()&0o111 != 0 {
			exec = "1"
		}
		fmt.Fprintf(h, "%s\x00regular\x00%s\x00%d\x00", strings.ReplaceAll(filepath.ToSlash(rel), "\\", "/"), exec, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
