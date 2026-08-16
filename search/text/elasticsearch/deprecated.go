package elasticsearch

import (
	textsearch "github.com/primandproper/platform-go/v11/search/text"
)

// The refusals this backend used to name itself, kept as aliases for the values
// they became when they moved to search/text. They are the same values, so
// errors.Is matches through either name and a caller written against v10.0.0
// keeps compiling; search/text/errors.go carries the reasoning for the move and
// the documentation for each. Both names are removed in the next major.
var (
	// Deprecated: use textsearch.ErrEmptyQueryProvided. The refusal is about the
	// request, not this backend, and Algolia raises the same one.
	ErrEmptyQueryProvided = textsearch.ErrEmptyQueryProvided

	// Deprecated: use textsearch.ErrResultWindowExceeded. Elasticsearch's ceiling
	// is index.max_result_window, but the refusal a caller answers — narrow the
	// query, this index will not page that deep — is not particular to it.
	ErrResultWindowExceeded = textsearch.ErrResultWindowExceeded
)
