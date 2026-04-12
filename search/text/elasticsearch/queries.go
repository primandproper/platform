package elasticsearch

import "encoding/json"

type matchCondition struct {
	Query string `json:"query"`
}

type matchQuery map[string]matchCondition

type wildcardCondition struct {
	Value string `json:"value"`
}

type wildcardQuery map[string]wildcardCondition

type condition struct {
	Match    matchQuery     `json:"match,omitempty"`
	Wildcard *wildcardQuery `json:"wildcard,omitempty"`
}

type should struct {
	Should []condition `json:"should"`
}

type queryContainer struct {
	Bool should `json:"bool"`
}

type searchQuery struct {
	Query queryContainer `json:"query"`
}

type esHit struct {
	ID         string          `json:"_id"`
	Source     json.RawMessage `json:"_source"`
	Highlights json.RawMessage `json:"highlight"`
	Sort       []any           `json:"sort"`
}

type esResponse struct {
	Hits struct {
		Hits  []*esHit
		Total struct{ Value int }
	}
}
