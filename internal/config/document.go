package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

var (
	ErrConflict   = errors.New("config changed on disk")
	ErrValidation = errors.New("invalid config document")
	ErrUnsafePath = errors.New("unsafe config path")
)

// Fingerprint identifies the exact bytes from which a Document was loaded.
type Fingerprint struct {
	Exists bool
	SHA256 [sha256.Size]byte
}

// ConflictError says saving would overwrite a file other than the one loaded.
type ConflictError struct {
	Path   string
	Reason string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("config %q changed on disk: %s", e.Path, e.Reason)
}
func (e *ConflictError) Unwrap() error { return ErrConflict }

// ValidationError says a requested edit or rendered candidate is not valid config.
type ValidationError struct {
	Path string
	Err  error
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("invalid config document: %v", e.Err)
	}
	return fmt.Sprintf("invalid config document %q: %v", e.Path, e.Err)
}
func (e *ValidationError) Unwrap() error        { return e.Err }
func (e *ValidationError) Is(target error) bool { return target == ErrValidation }

// UnsafePathError reports a destination atomic replacement must not touch.
type UnsafePathError struct {
	Path   string
	Reason string
	Err    error
}

func (e *UnsafePathError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("unsafe config path %q: %s: %v", e.Path, e.Reason, e.Err)
	}
	return fmt.Sprintf("unsafe config path %q: %s", e.Path, e.Reason)
}
func (e *UnsafePathError) Unwrap() error        { return e.Err }
func (e *UnsafePathError) Is(target error) bool { return target == ErrUnsafePath }

// Document holds the original lines and pending edits without normalizing untouched text.
type Document struct {
	path        string
	fingerprint Fingerprint
	lines       []documentLine
	defaultEOL  string
}

type documentLine struct {
	text string
	eol  string
}

// LoadDocument loads an existing valid config, or an empty document associated with a missing
// path. Existing content is parsed before it can be edited, so unknown schema is never retained.
func LoadDocument(path string) (*Document, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, &UnsafePathError{Path: path, Reason: "destination is a symbolic link"}
		}
		if !info.Mode().IsRegular() {
			return nil, &UnsafePathError{Path: path, Reason: "destination is not a regular file"}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if _, err := Parse(path, data); err != nil {
			return nil, &ValidationError{Path: path, Err: err}
		}
		d := newDocument(path, data)
		d.fingerprint = fingerprint(data, true)
		return d, nil
	case errors.Is(err, fs.ErrNotExist):
		// Windows reports a path below a regular file as not-exist instead of
		// the not-a-directory error Unix gives, so the parent is checked here:
		// handing back an empty document would promise a Save that cannot
		// succeed, because the parent it would create is already a file.
		if parent := filepath.Dir(path); parent != path {
			if info, err := os.Lstat(parent); err == nil && !info.IsDir() {
				return nil, &UnsafePathError{Path: path, Reason: "parent is not a directory"}
			}
		}
		return newDocument(path, nil), nil
	default:
		return nil, err
	}
}

// Path is the destination associated with the document.
func (d *Document) Path() string { return d.path }

// Fingerprint is the disk state against which Save performs conflict detection.
func (d *Document) Fingerprint() Fingerprint { return d.fingerprint }

func fingerprint(data []byte, exists bool) Fingerprint {
	fp := Fingerprint{Exists: exists}
	if exists {
		fp.SHA256 = sha256.Sum256(data)
	}
	return fp
}

func newDocument(path string, data []byte) *Document {
	d := &Document{path: path, defaultEOL: "\n"}
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			d.lines = append(d.lines, documentLine{text: string(data)})
			break
		}
		text, eol := data[:i], "\n"
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text, eol = text[:len(text)-1], "\r\n"
		}
		d.lines = append(d.lines, documentLine{text: string(text), eol: eol})
		if len(d.lines) == 1 {
			d.defaultEOL = eol
		}
		data = data[i+1:]
	}
	return d
}

// Bytes renders the candidate exactly, including every original line ending not touched by an
// insertion. The returned slice is independent of the document.
func (d *Document) Bytes() []byte {
	var b strings.Builder
	for _, line := range d.lines {
		b.WriteString(line.text)
		b.WriteString(line.eol)
	}
	return []byte(b.String())
}

// Render validates the complete candidate through the strict parser and real builders.
func (d *Document) Render() ([]byte, error) {
	data := d.Bytes()
	if _, err := Parse(d.path, data); err != nil {
		return nil, &ValidationError{Path: d.path, Err: err}
	}
	return data, nil
}

// SetString changes one supported persistent string setting.
func (d *Document) SetString(section, key, value string) error {
	if section != "input" || key != "title" {
		return unsupportedSetting(section, key)
	}
	return d.set(section, key, quoteString(value))
}

// SetBool changes one supported persistent boolean setting.
func (d *Document) SetBool(section, key string, value bool) error {
	if !((section == "scroll" && key == "mouse") || (section == "anim" && key == "shine")) {
		return unsupportedSetting(section, key)
	}
	return d.set(section, key, strconv.FormatBool(value))
}

// SetPositiveInt changes one supported persistent positive integer setting.
func (d *Document) SetPositiveInt(section, key string, value int) error {
	valid := section == "scroll" && key == "lines"
	valid = valid || section == "anim" && (key == "period" || key == "travel" || key == "width")
	if !valid {
		return unsupportedSetting(section, key)
	}
	if value < 1 {
		return &ValidationError{Path: d.path, Err: fmt.Errorf("[%s] %s must be 1 or more", section, key)}
	}
	return d.set(section, key, strconv.Itoa(value))
}

func unsupportedSetting(section, key string) error {
	return &ValidationError{Err: fmt.Errorf("[%s] %s is not a persistent common setting", section, key)}
}

func quoteString(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}

func (d *Document) set(section, key, encoded string) error {
	sectionLine, nextSection, keyLine := d.locate(section, key)
	if keyLine >= 0 {
		d.lines[keyLine].text = replaceValue(d.lines[keyLine].text, encoded)
		return nil
	}
	if sectionLine >= 0 {
		at := nextSection
		for at > sectionLine+1 && strings.TrimSpace(d.lines[at-1].text) == "" {
			at--
		}
		d.insert(at, documentLine{text: key + " = " + encoded, eol: d.defaultEOL})
		return nil
	}
	if len(d.lines) > 0 {
		if d.lines[len(d.lines)-1].eol == "" {
			d.lines[len(d.lines)-1].eol = d.defaultEOL
		}
		if strings.TrimSpace(d.lines[len(d.lines)-1].text) != "" {
			d.lines = append(d.lines, documentLine{eol: d.defaultEOL})
		}
	}
	d.lines = append(d.lines,
		documentLine{text: "[" + section + "]", eol: d.defaultEOL},
		documentLine{text: key + " = " + encoded, eol: d.defaultEOL})
	return nil
}

func (d *Document) locate(section, key string) (sectionLine, nextSection, keyLine int) {
	sectionLine, nextSection, keyLine = -1, len(d.lines), -1
	current := ""
	for i, line := range d.lines {
		text := strings.TrimSpace(line.text)
		if strings.HasPrefix(text, "[") {
			head := strings.TrimSpace(cutComment(text))
			if strings.HasSuffix(head, "]") {
				current = strings.TrimSpace(head[1 : len(head)-1])
				if sectionLine >= 0 && current != section {
					nextSection = i
					break
				}
				if current == section {
					sectionLine = i
				}
			}
			continue
		}
		if current != section || text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		name, _, ok := strings.Cut(text, "=")
		if ok && strings.Trim(strings.TrimSpace(name), `"`) == key {
			keyLine = i
		}
	}
	return
}

func replaceValue(line, encoded string) string {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return line
	}
	start := eq + 1
	for start < len(line) && (line[start] == ' ' || line[start] == '\t') {
		start++
	}
	end := valueEnd(line, start)
	trimmed := end
	for trimmed > start && (line[trimmed-1] == ' ' || line[trimmed-1] == '\t') {
		trimmed--
	}
	return line[:start] + encoded + line[trimmed:]
}

func valueEnd(line string, start int) int {
	quoted, escaped := false, false
	for i := start; i < len(line); i++ {
		c := line[i]
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' && i == start {
			quoted = true
			continue
		}
		if c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return i
		}
	}
	return len(line)
}

func (d *Document) insert(at int, line documentLine) {
	if at == len(d.lines) && at > 0 && d.lines[at-1].eol == "" {
		d.lines[at-1].eol = d.defaultEOL
	}
	if at < len(d.lines) {
		line.eol = d.lines[at].eol
		if line.eol == "" {
			line.eol = d.defaultEOL
		}
	}
	d.lines = append(d.lines, documentLine{})
	copy(d.lines[at+1:], d.lines[at:])
	d.lines[at] = line
}

// Save validates, checks the original fingerprint, and atomically replaces the destination.
func (d *Document) Save() error {
	data, err := d.Render()
	if err != nil {
		return err
	}
	current, mode, err := inspectDestination(d.path)
	if err != nil {
		return err
	}
	if err := d.checkConflict(current); err != nil {
		return err
	}
	if err := atomicWrite(d.path, data, mode); err != nil {
		return err
	}
	d.fingerprint = fingerprint(data, true)
	return nil
}

type diskState struct {
	exists bool
	data   []byte
}

func inspectDestination(path string) (diskState, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return diskState{}, 0o600, nil
	}
	if err != nil {
		return diskState{}, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return diskState{}, 0, &UnsafePathError{Path: path, Reason: "destination is a symbolic link"}
	}
	if !info.Mode().IsRegular() {
		return diskState{}, 0, &UnsafePathError{Path: path, Reason: "destination is not a regular file"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return diskState{}, 0, err
	}
	return diskState{exists: true, data: data}, info.Mode().Perm(), nil
}

func (d *Document) checkConflict(current diskState) error {
	if !d.fingerprint.Exists && current.exists {
		return &ConflictError{Path: d.path, Reason: "missing file appeared"}
	}
	if d.fingerprint.Exists && !current.exists {
		return &ConflictError{Path: d.path, Reason: "existing file was removed"}
	}
	if current.exists && sha256.Sum256(current.data) != d.fingerprint.SHA256 {
		return &ConflictError{Path: d.path, Reason: "contents changed"}
	}
	return nil
}

func atomicWrite(path string, data []byte, mode fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := secureMkdirAll(dir); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return &UnsafePathError{Path: dir, Reason: "parent is not a directory"}
	}
	tmp, err := os.CreateTemp(dir, ".arxi-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmp != nil {
			_ = tmp.Close()
		}
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		tmp = nil
		return err
	}
	tmp = nil
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if f, openErr := os.Open(dir); openErr != nil {
			return openErr
		} else {
			err = f.Sync()
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	return err
}

func secureMkdirAll(dir string) error {
	dir = filepath.Clean(dir)
	parent := filepath.Dir(dir)
	if parent != dir {
		if err := secureMkdirAll(parent); err != nil {
			return err
		}
	}
	info, err := os.Lstat(dir)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return &UnsafePathError{Path: dir, Reason: "parent is not a directory"}
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err = os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return &UnsafePathError{Path: dir, Reason: "parent is not a directory"}
	}
	return nil
}
