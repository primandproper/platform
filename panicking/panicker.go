package panicking

import (
	"fmt"
)

// Panicker abstracts panic for our tests and such.
type Panicker interface {
	Panic(any)
	Panicf(format string, args ...any)
}

// NewProductionPanicker produces a production-ready panicker that will actually panic when called.
func NewProductionPanicker() *StandardPanicker {
	return &StandardPanicker{}
}

var _ Panicker = (*StandardPanicker)(nil)

// StandardPanicker is the Panicker that actually panics. It is exported, and
// returned by NewProductionPanicker, so a caller can depend on the panicker it
// built rather than on the Panicker seam.
type StandardPanicker struct{}

func (p *StandardPanicker) Panic(msg any) {
	panic(msg)
}

func (p *StandardPanicker) Panicf(format string, args ...any) {
	p.Panic(fmt.Sprintf(format, args...))
}
