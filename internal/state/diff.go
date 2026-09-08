package state

import (
	"strconv"
	"strings"

	"arxi.local/sim/internal/event"
)

// A diff is the one thing a transcript can show that a summary cannot: not that a
// file changed, but what it now says. The wire carries unified-diff rows because
// that is what an edit tool already has in hand; this file turns them into
// something a renderer can put a number and a colour on, once, at fold time.

// DiffOp is what one row of a hunk does to the file.
type DiffOp uint8

const (
	DiffContext DiffOp = iota
	DiffAdded
	DiffRemoved
)

// DiffRow is one line of a hunk with both of its line numbers resolved.
//
// A removed line has no number on the new side and an added line has none on the
// old side, so the unused one is left at zero rather than guessed at. Num is the
// number a gutter should print, and the rule it encodes — old for a removal, new
// for everything else — is the whole numbering convention of the view.
type DiffRow struct {
	Op   DiffOp
	Old  int
	New  int
	Text string
}

// Num is the line number to print for this row.
func (r DiffRow) Num() int {
	if r.Op == DiffRemoved {
		return r.Old
	}
	return r.New
}

// DiffHunk is a contiguous run of rows. Two hunks in one diff mean the edit
// touched two places in the file, and the space between them is a fact worth
// drawing rather than a gap to close silently.
type DiffHunk struct {
	Rows []DiffRow
}

// Diff is one file's worth of change.
//
// Added and Removed are counted here rather than read off the wire, because a
// number that can be derived is a number a hand-written log can get wrong.
type Diff struct {
	Path    string
	Lang    string
	Added   int
	Removed int
	Hunks   []DiffHunk
}

// Summary is the one-line wording an edit reports when the log did not report its
// own. "line" is singular at one, and a clause that would say zero is dropped
// instead of printed: "Added 3 lines" is what an insertion did, and saying it
// removed nothing is noise.
func (d *Diff) Summary() string {
	var parts []string
	if d.Added > 0 {
		parts = append(parts, "Added "+plural(d.Added, "line"))
	}
	if d.Removed > 0 {
		parts = append(parts, "removed "+plural(d.Removed, "line"))
	}
	if len(parts) == 0 {
		return "No lines changed"
	}
	return strings.Join(parts, ", ")
}

func plural(n int, word string) string {
	s := strconv.Itoa(n) + " " + word
	if n != 1 {
		s += "s"
	}
	return s
}

// buildDiff turns the wire shape into the view shape, or returns nil for a result
// that carried no diff — which is most of them.
//
// The row strings are unified-diff lines, so the first byte is the operation and
// the rest is the text. A row that is empty at all is context: an unchanged blank
// line loses its leading space to any editor that trims, and refusing to read it
// would turn a whitespace slip in a hand-written scenario into a missing row.
func buildDiff(p *event.DiffPayload) *Diff {
	if p == nil {
		return nil
	}
	d := &Diff{Path: p.Path, Lang: p.Lang}
	for _, h := range p.Hunks {
		oldNo, newNo := h.OldLine, h.NewLine
		if oldNo <= 0 {
			oldNo = 1
		}
		if newNo <= 0 {
			newNo = 1
		}
		hunk := DiffHunk{}
		for _, raw := range h.Rows {
			row := DiffRow{Op: DiffContext, Text: raw}
			if raw != "" {
				switch raw[0] {
				case '+':
					row.Op, row.Text = DiffAdded, raw[1:]
				case '-':
					row.Op, row.Text = DiffRemoved, raw[1:]
				case ' ':
					row.Text = raw[1:]
				}
			}
			switch row.Op {
			case DiffAdded:
				row.New = newNo
				newNo++
				d.Added++
			case DiffRemoved:
				row.Old = oldNo
				oldNo++
				d.Removed++
			default:
				row.Old, row.New = oldNo, newNo
				oldNo++
				newNo++
			}
			hunk.Rows = append(hunk.Rows, row)
		}
		if len(hunk.Rows) > 0 {
			d.Hunks = append(d.Hunks, hunk)
		}
	}
	if len(d.Hunks) == 0 {
		return nil
	}
	return d
}
