package textsearch

// IndexRequest is the message an indexer consumes: which row, of which type, to
// index or delete.
//
// The fields are exactly what indexing needs. Anything an application wants to
// correlate — a test identifier, a tenant, a trace — belongs in the message
// envelope or the span, not in this module's wire format, where every consumer
// of every application pays for it.
type IndexRequest struct {
	RequestID string `json:"id"`
	RowID     string `json:"rowID"`
	IndexType string `json:"type"`
	Delete    bool   `json:"delete"`
}
