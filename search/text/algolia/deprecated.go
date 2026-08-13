package algolia

import (
	textsearch "github.com/primandproper/platform-go/v10/search/text"
)

// The refusal this backend used to name itself, kept as an alias for the value
// it became when it moved to search/text. It is the same value, so errors.Is
// matches through either name and a caller written against v10.0.0 keeps
// compiling; search/text/errors.go carries the reasoning for the move. The name
// here is removed in the next major.
var (
	// Deprecated: use textsearch.ErrEmptyQueryProvided. The refusal is about the
	// request, not this backend, and Elasticsearch raises the same one.
	ErrEmptyQueryProvided = textsearch.ErrEmptyQueryProvided
)
