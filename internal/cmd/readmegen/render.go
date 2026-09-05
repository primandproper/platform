package main

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// The two marks a matrix cell may hold.
const (
	supported   = "✓"
	unsupported = "—"
)

// The three generated regions of the README, named by the marker pair that
// delimits each. They are separate regions rather than one because the prose
// between them is the argument the tables are evidence for, and it is written
// by hand.
const (
	transportsRegion = "transports"
	dialectsRegion   = "dialects"
	narrowingsRegion = "narrowings"
)

// regions renders every generated region of the README from one survey, keyed
// by region name. Each body ends in a newline, so a region is whole lines.
func (s *survey) regions() map[string]string {
	return map[string]string{
		transportsRegion: s.transportsTable(),
		dialectsRegion:   s.dialectsTable(),
		narrowingsRegion: s.narrowingsList(),
	}
}

// transportsTable is the roster of everything this module ships on the far side
// of the store/transport line, with the kind and the prose each package's own
// doc.go carries about it.
func (s *survey) transportsTable() string {
	rows := make([][]string, 0, len(s.transports))

	for i := range s.transports {
		t := &s.transports[i]
		rows = append(rows, []string{code(t.pkg), t.kind, t.shape})
	}

	return table([]string{"Transport", "Kind", "Whose shape it is"}, rows)
}

// dialectsTable is the roster of which dialects each store-shipping package
// ships DDL for, which is read off its migrations directory and nowhere else.
func (s *survey) dialectsTable() string {
	header := []string{"Package"}
	for _, d := range dialects {
		header = append(header, dialectColumns[d])
	}

	rows := make([][]string, 0, len(s.stores))

	for i := range s.stores {
		st := &s.stores[i]
		row := []string{code(st.pkg)}

		for _, d := range dialects {
			mark := unsupported
			if slices.Contains(st.dialects, d) {
				mark = supported
			}

			row = append(row, mark)
		}

		rows = append(rows, row)
	}

	return table(header, rows)
}

// narrowingsList is one line per package that ships fewer than three dialects,
// carrying the reason its own doc.go gives. A package with a full roster
// contributes nothing, so the list is exactly as long as the narrowing is real.
func (s *survey) narrowingsList() string {
	var b strings.Builder

	for i := range s.stores {
		st := &s.stores[i]
		if st.narrowing == "" {
			continue
		}

		b.WriteString("- " + code(st.pkg) + " — " + st.narrowing + ".\n")
	}

	return b.String()
}

// code wraps a package path in the backticks every row of both tables carries.
func code(s string) string {
	return "`" + s + "`"
}

// table renders a markdown table padded to its widest cell, which is the shape
// the README's hand-written tables are already in — a generated section that
// reformatted itself would make every table in the file look like two files.
func table(header []string, rows [][]string) string {
	widths := make([]int, len(header))

	for i, cell := range header {
		widths[i] = utf8.RuneCountInString(cell)
	}

	for _, row := range rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder

	writeRow(&b, header, widths)

	rule := make([]string, len(widths))
	for i, w := range widths {
		rule[i] = strings.Repeat("-", w+2)
	}

	b.WriteString("|" + strings.Join(rule, "|") + "|\n")

	for _, row := range rows {
		writeRow(&b, row, widths)
	}

	return b.String()
}

func writeRow(b *strings.Builder, cells []string, widths []int) {
	for i, cell := range cells {
		b.WriteString("| " + cell + strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)) + " ")
	}

	b.WriteString("|\n")
}
