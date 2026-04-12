package encryption

import (
	"github.com/primandproper/platform/errors"
)

var (
	ErrIncorrectKeyLength = errors.New("secret is not the right length")
)
