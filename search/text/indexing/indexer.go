package indexing

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/messagequeue"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/keys"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/random"
	textsearch "github.com/primandproper/platform-go/v9/search/text"

	"github.com/hashicorp/go-multierror"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	serviceName = "indexer"
)

type Function func(context.Context) ([]string, error)

// IndexScheduler picks a registered index type and publishes indexing requests for it. The
// indexFunctions map is populated at construction and never mutated afterwards, so reads of it
// need no synchronization.
type IndexScheduler struct {
	o11y                     observability.Observer
	handledRecordsCounter    metrics.Int64Counter
	searchDataIndexPublisher messagequeue.Publisher
	indexFunctions           map[string]Function
	allIndexTypes            []string
}

// NewIndexScheduler builds a scheduler that publishes index requests to topic.
//
// The topic is a plain string rather than a field on a shared queue-catalog
// config: which topic carries index requests is the application's decision, and
// naming it in this module's config made every consumer inherit one
// application's topic names.
func NewIndexScheduler(
	ctx context.Context,
	messageQueuePublisherProvider messagequeue.PublisherProvider,
	topic string,
	indexFunctions map[string]Function,
	opts ...Option,
) (*IndexScheduler, error) {
	o := newOptions(opts)

	handledRecordsCounter, err := metrics.EnsureMetricsProvider(o.metricsProvider).NewInt64Counter(fmt.Sprintf("%s.handled_records", serviceName))
	if err != nil {
		return nil, err
	}

	searchDataIndexPublisher, err := messageQueuePublisherProvider.NewPublisher(ctx, topic)
	if err != nil {
		return nil, err
	}

	indexFunctionsMap := indexFunctions
	if indexFunctions == nil {
		indexFunctionsMap = make(map[string]Function)
	}

	allIndexTypes := []string{}
	for k := range indexFunctionsMap {
		allIndexTypes = append(allIndexTypes, k)
	}

	return &IndexScheduler{
		handledRecordsCounter:    handledRecordsCounter,
		searchDataIndexPublisher: searchDataIndexPublisher,
		o11y:                     observability.NewObserver(serviceName, o.logger, o.tracerProvider),

		allIndexTypes:  allIndexTypes,
		indexFunctions: indexFunctionsMap,
	}, nil
}

func (i *IndexScheduler) IndexTypes(ctx context.Context) error {
	ctx, op := i.o11y.Begin(ctx)
	defer op.End()

	// figure out what records to join
	chosenIndex := random.Element(i.allIndexTypes)

	op.Set(keys.IndexNameKey, chosenIndex)
	op.Logger().Info("index type chosen")

	actionFunc, ok := i.indexFunctions[chosenIndex]
	if !ok {
		return errors.Newf("unknown index type %s", chosenIndex)
	}

	ids, err := actionFunc(ctx)
	if err != nil {
		if !stderrors.Is(err, sql.ErrNoRows) {
			op.Acknowledge(err, "getting %s IDs that need search indexing", chosenIndex)
			return err
		}
		return nil
	}

	if len(ids) > 0 {
		op.Set("count", len(ids))
		op.Logger().Info("publishing search index requests")
	}

	publishedIDCount := int64(0)
	errs := &multierror.Error{}
	for _, id := range ids {
		indexReq := &textsearch.IndexRequest{
			RowID:     id,
			IndexType: chosenIndex,
		}
		if err = i.searchDataIndexPublisher.Publish(ctx, indexReq); err != nil {
			errs = multierror.Append(errs, err)
		} else {
			publishedIDCount++
		}
	}

	i.handledRecordsCounter.Add(ctx, publishedIDCount, metric.WithAttributes(
		attribute.KeyValue{
			Key:   "record.type",
			Value: attribute.StringValue(chosenIndex),
		},
	))

	op.Set("published.count", publishedIDCount)

	return errs.ErrorOrNil()
}
