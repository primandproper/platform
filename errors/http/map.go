package http

import (
	"sync"

	"github.com/primandproper/platform/types"
)

// HTTPErrorMapper maps domain errors to (ErrorCode, message). ok=false means no match.
type HTTPErrorMapper interface {
	Map(err error) (code types.ErrorCode, msg string, ok bool)
}

var (
	domainMappers   []HTTPErrorMapper
	domainMappersMu sync.RWMutex
)

// RegisterHTTPErrorMapper registers a domain-specific error mapper.
// Domains call this from init() to contribute their error mappings.
func RegisterHTTPErrorMapper(m HTTPErrorMapper) {
	domainMappersMu.Lock()
	defer domainMappersMu.Unlock()
	domainMappers = append(domainMappers, m)
}

// ToAPIError maps known sentinel errors to types.ErrorCode and a safe user-facing message.
// It tries PlatformMapper first, then each registered domain mapper.
// Returns (code, message). Use types.ErrTalkingToDatabase and "an error occurred" as fallback for unknown errors.
func ToAPIError(err error) (code types.ErrorCode, msg string) {
	if err == nil {
		return types.ErrNothingSpecific, ""
	}
	if c, m, ok := PlatformMapper.Map(err); ok {
		return c, m
	}
	domainMappersMu.RLock()
	mappers := domainMappers
	domainMappersMu.RUnlock()
	for _, mapper := range mappers {
		if c, m, ok := mapper.Map(err); ok {
			return c, m
		}
	}
	return types.ErrTalkingToDatabase, "an error occurred"
}
