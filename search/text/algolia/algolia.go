package algolia

import (
	"fmt"

	"github.com/primandproper/platform-go/v8/circuitbreaking"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability"
	textsearch "github.com/primandproper/platform-go/v8/search/text"

	algolia "github.com/algolia/algoliasearch-client-go/v3/algolia/search"
)

var (
	_ textsearch.Index[any] = (*indexManager[any])(nil)

	ErrNilConfig = platformerrors.New("nil config provided")
)

type (
	indexManager[T any] struct {
		o11y           observability.Observer
		circuitBreaker circuitbreaking.CircuitBreaker
		client         *algolia.Index
	}
)

func NewIndexManager[T any](
	cfg *Config,
	indexName string,
	circuitBreaker circuitbreaking.CircuitBreaker,
	opts ...Option,
) (textsearch.Index[T], error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	o := newOptions(opts)

	clientConfig := algolia.Configuration{
		AppID:  cfg.AppID,
		APIKey: cfg.APIKey,
	}
	// Honor a configured timeout for both read and write operations; leave the
	// SDK's own defaults in place when unset.
	if cfg.Timeout > 0 {
		clientConfig.ReadTimeout = cfg.Timeout
		clientConfig.WriteTimeout = cfg.Timeout
	}

	im := &indexManager[T]{
		o11y:           observability.NewObserver(fmt.Sprintf("search_%s", indexName), o.logger, o.tracerProvider),
		client:         algolia.NewClientWithConfig(clientConfig).InitIndex(indexName),
		circuitBreaker: circuitBreaker,
	}

	return im, nil
}
