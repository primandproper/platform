package encoding

// NewContentType resolves the ContentType named by a Config.
//
// An unrecognized content type is an error rather than a silent fall back to
// JSON: a typo in configuration should stop startup, not quietly change the
// wire format of every response.
func NewContentType(cfg Config) (ContentType, error) {
	return ParseContentType(cfg.ContentType)
}
