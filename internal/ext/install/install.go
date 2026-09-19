// Package install safely installs unpacked local extensions.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"arxi.local/sim/internal/ext"
)

const ManifestName = "manifest.toml"

var ErrUnsafePath = errors.New("unsafe extension path")

// Limits bounds source traversal. Zero values use conservative defaults.
type Limits struct {
	MaxFiles int
	MaxDepth int
	MaxBytes int64
}

// Options controls preflight. Root defaults to ExtensionsDir().
type Options struct {
	Root   string
	Limits Limits
}

// Plan is an immutable preflight snapshot. Access it through its methods.
type Plan struct {
	manifest                          ext.Manifest
	source, root, destination, digest string
	files                             []fileRecord
	bytes                             int64
}

type fileRecord struct {
	rel  string
	size int64
	mode fs.FileMode
	sum  [32]byte
}

func (p Plan) Manifest() ext.Manifest { return cloneManifest(p.manifest) }
func (p Plan) Source() string         { return p.source }
func (p Plan) Root() string           { return p.root }
func (p Plan) Destination() string    { return p.destination }
func (p Plan) Digest() string         { return p.digest }
func (p Plan) FileCount() int         { return len(p.files) }
func (p Plan) Bytes() int64           { return p.bytes }

// Result describes a committed generation.
type Result struct {
	Created      bool
	ManifestPath string
	generation   string
}

// ExtensionsDir returns the per-user extension store.
func ExtensionsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "arxi-sim", "extensions"), nil
}

func defaults(l Limits) Limits {
	if l.MaxFiles == 0 {
		l.MaxFiles = 4096
	}
	if l.MaxDepth == 0 {
		l.MaxDepth = 32
	}
	if l.MaxBytes == 0 {
		l.MaxBytes = 256 << 20
	}
	return l
}

// Preflight validates and snapshots an unpacked source directory.
func Preflight(source string, options ...Options) (Plan, error) {
	var o Options
	if len(options) > 1 {
		return Plan{}, errors.New("at most one Options value is allowed")
	}
	if len(options) == 1 {
		o = options[0]
	}
	if o.Root == "" {
		var err error
		o.Root, err = ExtensionsDir()
		if err != nil {
			return Plan{}, err
		}
	}
	l := defaults(o.Limits)
	if l.MaxFiles < 1 || l.MaxDepth < 1 || l.MaxBytes < 1 {
		return Plan{}, errors.New("limits must be positive")
	}
	src, err := filepath.Abs(source)
	if err != nil {
		return Plan{}, err
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return Plan{}, err
	}
	if nested(src, root) || nested(root, src) {
		return Plan{}, fmt.Errorf("source and destination root overlap: %w", ErrUnsafePath)
	}
	info, err := os.Lstat(src)
	if err != nil {
		return Plan{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
		return Plan{}, fmt.Errorf("source must be a real directory: %w", ErrUnsafePath)
	}
	var records []fileRecord
	var total int64
	seen := map[string]string{}
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if !safeRelative(rel) {
			return fmt.Errorf("%q: %w", rel, ErrUnsafePath)
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) > l.MaxDepth {
			return fmt.Errorf("maximum depth exceeded")
		}
		key := strings.ToLower(filepath.ToSlash(rel))
		if prior, ok := seen[key]; ok {
			return fmt.Errorf("portable path collision %q and %q: %w", prior, rel, ErrUnsafePath)
		}
		seen[key] = rel
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return fmt.Errorf("%q is a link: %w", rel, ErrUnsafePath)
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%q is not regular: %w", rel, ErrUnsafePath)
		}
		if len(records)+1 > l.MaxFiles {
			return fmt.Errorf("maximum files exceeded")
		}
		if info.Size() < 0 || total > l.MaxBytes-info.Size() {
			return fmt.Errorf("maximum bytes exceeded")
		}
		sum, err := hashFile(path)
		if err != nil {
			return err
		}
		records = append(records, fileRecord{rel: filepath.ToSlash(rel), size: info.Size(), mode: info.Mode(), sum: sum})
		total += info.Size()
		return nil
	})
	if err != nil {
		return Plan{}, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].rel < records[j].rel })
	manifestPath := filepath.Join(src, ManifestName)
	manifest, err := ext.LoadManifest(manifestPath)
	if err != nil {
		return Plan{}, err
	}
	if err := validateExecutable(src, manifest.Executable); err != nil {
		return Plan{}, err
	}
	digest, err := ext.TreeDigest(src)
	if err != nil {
		return Plan{}, err
	}
	destination := filepath.Join(root, manifest.Name, "generations", digest)
	return Plan{manifest: cloneManifest(manifest), source: src, root: root, destination: destination, digest: digest, files: append([]fileRecord(nil), records...), bytes: total}, nil
}

func cloneManifest(m ext.Manifest) ext.Manifest {
	m.Args = append([]string(nil), m.Args...)
	m.Capabilities = append([]ext.Capability(nil), m.Capabilities...)
	return m
}

func safeRelative(rel string) bool {
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return false
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." || part == ".." || strings.Contains(part, ":") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || windowsReserved(part) {
			return false
		}
	}
	return true
}

func windowsReserved(part string) bool {
	base := strings.ToLower(strings.TrimSuffix(part, filepath.Ext(part)))
	if base == "con" || base == "prn" || base == "aux" || base == "nul" || base == "clock$" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}

func nested(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func validateExecutable(source, executable string) error {
	if executable == "" || filepath.IsAbs(executable) || filepath.VolumeName(executable) != "" || (!strings.Contains(executable, "/") && !strings.Contains(executable, `\`)) || !safeRelative(executable) {
		return fmt.Errorf("executable must be an explicit contained relative path: %w", ErrUnsafePath)
	}
	path := filepath.Join(source, filepath.FromSlash(strings.ReplaceAll(executable, `\`, "/")))
	if !nested(path, source) {
		return fmt.Errorf("executable escapes source: %w", ErrUnsafePath)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
		return fmt.Errorf("executable is not a regular file: %w", ErrUnsafePath)
	}
	return nil
}

func hashFile(path string) ([32]byte, error) {
	var zero [32]byte
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return zero, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

func digestRecords(records []fileRecord) string {
	h := sha256.New()
	for _, r := range records {
		fmt.Fprintf(h, "%s\x00%d\x00%03o\x00", r.rel, r.size, r.mode.Perm()&0111)
		h.Write(r.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Commit copies, verifies, and atomically publishes a preflight plan.
func Commit(p Plan) (Result, error) {
	if p.source == "" || p.destination == "" || p.digest == "" {
		return Result{}, errors.New("invalid plan")
	}
	if err := ensureSafeParents(p.root, filepath.Dir(p.destination)); err != nil {
		return Result{}, err
	}
	if _, err := os.Lstat(p.destination); err == nil {
		if err := verifyTree(p.destination, p.files, p.digest); err != nil {
			return Result{}, fmt.Errorf("existing generation collision: %w", err)
		}
		return Result{ManifestPath: filepath.Join(p.destination, ManifestName), generation: p.destination}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, err
	}
	parent := filepath.Dir(p.destination)
	stage, err := os.MkdirTemp(parent, ".stage-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0700); err != nil {
		return Result{}, err
	}
	for _, record := range p.files {
		src := filepath.Join(p.source, filepath.FromSlash(record.rel))
		dst := filepath.Join(stage, filepath.FromSlash(record.rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return Result{}, err
		}
		if err := copyExclusive(src, dst, 0600|(record.mode.Perm()&0111)); err != nil {
			return Result{}, err
		}
	}
	if err := verifyTree(stage, p.files, p.digest); err != nil {
		return Result{}, fmt.Errorf("staged verification: %w", err)
	}
	if err := os.Rename(stage, p.destination); err != nil {
		if _, statErr := os.Lstat(p.destination); statErr == nil && verifyTree(p.destination, p.files, p.digest) == nil {
			return Result{ManifestPath: filepath.Join(p.destination, ManifestName), generation: p.destination}, nil
		}
		return Result{}, err
	}
	return Result{Created: true, ManifestPath: filepath.Join(p.destination, ManifestName), generation: p.destination}, nil
}

// Rollback removes only a generation created by this result.
func Rollback(result Result) error {
	if !result.Created || result.generation == "" {
		return nil
	}
	return os.RemoveAll(result.generation)
}

func ensureSafeParents(root, target string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	if err := os.Chmod(root, 0700); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || !safeRelative(rel) {
		return fmt.Errorf("destination escapes root: %w", ErrUnsafePath)
	}
	cur := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(cur, 0700); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return fmt.Errorf("unsafe destination component %q: %w", cur, ErrUnsafePath)
		}
	}
	return nil
}

func copyExclusive(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func verifyTree(root string, expected []fileRecord, digest string) error {
	got := make([]fileRecord, 0, len(expected))
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return ErrUnsafePath
		}
		if d.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return ErrUnsafePath
		}
		sum, err := hashFile(path)
		if err != nil {
			return err
		}
		got = append(got, fileRecord{rel: filepath.ToSlash(rel), size: info.Size(), mode: info.Mode(), sum: sum})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(got, func(i, j int) bool { return got[i].rel < got[j].rel })
	actual, err := ext.TreeDigest(root)
	if err != nil {
		return err
	}
	if actual != digest || len(got) != len(expected) {
		return errors.New("tree digest mismatch")
	}
	return nil
}

func isReparse(info fs.FileInfo) bool { return reparsePoint(info) }
