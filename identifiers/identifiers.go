package identifiers

import (
	"github.com/rs/xid"
)

// New produces a new string ID.
func New() string {
	return xid.New().String()
}

// Validate validates a string ID.
func Validate(x string) error {
	_, err := xid.FromString(x)
	return err
}
