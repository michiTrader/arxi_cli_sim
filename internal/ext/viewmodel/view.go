// Package viewmodel defines transport-neutral extension panel values.
package viewmodel

type Role string

const (
	RoleText     Role = "text"
	RoleMuted    Role = "muted"
	RoleEmphasis Role = "emphasis"
	RoleSuccess  Role = "success"
	RoleWarning  Role = "warning"
	RoleError    Role = "error"
	RoleTitle    Role = "title"
	RoleCode     Role = "code"
)

type Span struct {
	Text string
	Role Role
}
type Row struct {
	ID    string
	Spans []Span
}
type View struct {
	ID            string
	Width, Height int
	Rows          []Row
}
type Input struct {
	Kind, Key, Action, Text     string
	X, Y, DX, DY, Width, Height int
}
