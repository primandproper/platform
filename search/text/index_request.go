package textsearch

type IndexRequest struct {
	RequestID string `json:"id"`
	RowID     string `json:"rowID"`
	IndexType string `json:"type"`
	TestID    string `json:"testID,omitempty"`
	Delete    bool   `json:"delete"`
}
