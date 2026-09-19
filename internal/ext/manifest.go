package ext

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
)

var ErrInvalidManifest = errors.New("invalid extension manifest")

// Manifest is the sim-local manifest.toml contract for one extension.
type Manifest struct {
	Name         string
	Version      string
	Protocol     string
	Executable   string
	Args         []string
	Capabilities []Capability
}

// ManifestError identifies a malformed field in a manifest.
type ManifestError struct {
	Path string
	Line int
	Err  error
}

func (e *ManifestError) Error() string {
	where := e.Path
	if where == "" {
		where = "manifest"
	}
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %v", where, e.Line, e.Err)
	}
	return fmt.Sprintf("%s: %v", where, e.Err)
}
func (e *ManifestError) Unwrap() error        { return e.Err }
func (e *ManifestError) Is(target error) bool { return target == ErrInvalidManifest }

// LoadManifest reads and parses a manifest from path.
func LoadManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	return ParseManifest(path, file)
}

// ParseManifest parses the deliberately small TOML subset used by manifests.
func ParseManifest(path string, reader io.Reader) (Manifest, error) {
	var manifest Manifest
	seen := make(map[string]int)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(stripComment(scanner.Text()))
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "[") {
			return Manifest{}, manifestError(path, line, "tables are not supported")
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return Manifest{}, manifestError(path, line, "expected key = value")
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if prior := seen[key]; prior != 0 {
			return Manifest{}, manifestError(path, line, fmt.Sprintf("duplicate key %q (first declared on line %d)", key, prior))
		}
		seen[key] = line
		var err error
		switch key {
		case "name":
			manifest.Name, err = parseString(value)
		case "version":
			manifest.Version, err = parseString(value)
		case "protocol":
			manifest.Protocol, err = parseString(value)
		case "executable":
			manifest.Executable, err = parseString(value)
		case "args":
			manifest.Args, err = parseStringArray(value)
		case "capabilities":
			var values []string
			values, err = parseStringArray(value)
			if err == nil {
				manifest.Capabilities = make([]Capability, len(values))
				for i, item := range values {
					manifest.Capabilities[i] = Capability(item)
				}
			}
		default:
			return Manifest{}, manifestError(path, line, fmt.Sprintf("unknown key %q", key))
		}
		if err != nil {
			return Manifest{}, manifestError(path, line, fmt.Sprintf("%s: %v", key, err))
		}
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, err
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, &ManifestError{Path: path, Err: err}
	}
	return manifest, nil
}

func manifestError(path string, line int, message string) error {
	return &ManifestError{Path: path, Line: line, Err: errors.New(message)}
}

func stripComment(text string) string {
	quoted, escaped := false, false
	for i, r := range text {
		if escaped {
			escaped = false
			continue
		}
		if quoted && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
		} else if r == '#' && !quoted {
			return text[:i]
		}
	}
	return text
}

func parseString(text string) (string, error) {
	if len(text) < 2 || text[0] != '"' || text[len(text)-1] != '"' {
		return "", errors.New("expected a quoted string")
	}
	value, err := strconv.Unquote(text)
	if err != nil {
		return "", fmt.Errorf("invalid quoted string: %w", err)
	}
	return value, nil
}

func parseStringArray(text string) ([]string, error) {
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil, errors.New("expected an array of quoted strings")
	}
	text = strings.TrimSpace(text[1 : len(text)-1])
	if text == "" {
		return []string{}, nil
	}
	var values []string
	for len(text) > 0 {
		text = strings.TrimSpace(text)
		if text == "" || text[0] != '"' {
			return nil, errors.New("expected a quoted string array item")
		}
		end, escaped := -1, false
		for i := 1; i < len(text); i++ {
			if escaped {
				escaped = false
				continue
			}
			if text[i] == '\\' {
				escaped = true
			} else if text[i] == '"' {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, errors.New("unterminated quoted string")
		}
		value, err := parseString(text[:end+1])
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		text = strings.TrimSpace(text[end+1:])
		if text == "" {
			break
		}
		if text[0] != ',' {
			return nil, errors.New("expected comma between array items")
		}
		text = text[1:]
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("trailing comma is not supported")
		}
	}
	return values, nil
}

// ValidateManifest checks required fields and closed vocabularies.
func ValidateManifest(manifest Manifest) error {
	if !validName(manifest.Name) {
		return fmt.Errorf("name must match [a-z][a-z0-9-]{0,62}: %w", ErrInvalidManifest)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("version is required: %w", ErrInvalidManifest)
	}
	if manifest.Protocol != "ext/v1" && manifest.Protocol != "ext/v2" {
		return fmt.Errorf("protocol must be %q or %q: %w", "ext/v1", "ext/v2", ErrInvalidManifest)
	}
	if strings.TrimSpace(manifest.Executable) == "" {
		return fmt.Errorf("executable is required: %w", ErrInvalidManifest)
	}
	seen := make(map[Capability]bool)
	for _, capability := range manifest.Capabilities {
		if !KnownCapabilityFor(manifest.Protocol, capability) {
			return fmt.Errorf("unknown capability %q: %w", capability, ErrInvalidManifest)
		}
		if seen[capability] {
			return fmt.Errorf("duplicate capability %q: %w", capability, ErrInvalidManifest)
		}
		seen[capability] = true
	}
	return nil
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, r := range name[1:] {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) && r != '-' {
			return false
		}
	}
	return true
}
