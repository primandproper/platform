package openai

import (
	"context"
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/llm"
	"github.com/primandproper/platform-go/v10/llm/internal/bridge"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/metrics"

	anyllm "github.com/mozilla-ai/any-llm-go"
	anyllmopenai "github.com/mozilla-ai/any-llm-go/providers/openai"
)

const (
	// name scopes this package's spans, logs, and metrics.
	name = "openai_llm"
	// providerName is what llm.Provider.Name reports. It is the vendor rather
	// than the component, since callers use it to reason about the model, and
	// it is stable enough to persist alongside stored completions.
	providerName = "openai"
	// fallbackModel is used when neither the request nor the config names one.
	fallbackModel = "gpt-4o-mini"
)

var _ llm.Provider = (*Provider)(nil)

// NewProvider creates a new OpenAI-backed LLM provider.
func NewProvider(cfg *Config, opts ...Option) (*Provider, error) {
	if cfg == nil {
		return nil, errors.New("openai config is required")
	}

	providerOpts := []anyllm.Option{
		anyllm.WithAPIKey(cfg.APIKey),
	}
	if cfg.BaseURL != "" {
		providerOpts = append(providerOpts, anyllm.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Timeout > 0 {
		providerOpts = append(providerOpts, anyllm.WithTimeout(cfg.Timeout))
	}

	provider, err := anyllmopenai.New(providerOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "create openai provider")
	}

	o := newOptions(opts)
	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	requestCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_requests", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating request counter")
	}

	errorCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_errors", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating error counter")
	}

	latencyHist, err := mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", name))
	if err != nil {
		return nil, errors.Wrap(err, "creating latency histogram")
	}

	return &Provider{
		o11y:           observability.NewObserver(name, o.logger, o.tracerProvider),
		requestCounter: requestCounter,
		errorCounter:   errorCounter,
		latencyHist:    latencyHist,
		provider:       provider,
		defaultModel:   cfg.DefaultModel,
	}, nil
}

// Provider is the OpenAI llm.Provider implementation. It is exported, and
// returned by NewProvider, so a caller who has chosen OpenAI can depend on that
// choice rather than on the interface every model provider shares.
type Provider struct {
	o11y           observability.Observer
	requestCounter metrics.Int64Counter
	errorCounter   metrics.Int64Counter
	latencyHist    metrics.Float64Histogram
	// provider is the interface rather than the concrete OpenAI provider, so
	// that the observability and translation seams around it can be exercised
	// without an HTTP round trip.
	provider     anyllm.Provider
	defaultModel string
}

// Name implements llm.Provider.
func (*Provider) Name() string {
	return providerName
}

// Capabilities implements llm.Provider.
func (*Provider) Capabilities() llm.Capabilities {
	return llm.Capabilities{
		Streaming:        true,
		Tools:            true,
		Images:           true,
		Reasoning:        true,
		StructuredOutput: true,
	}
}

// Completion implements llm.Provider.
//
// It does not retry. A rate limit comes back as an error matching
// llm.ErrRateLimited — usually a *llm.RateLimitError carrying the provider's
// advice about how long to wait — and choosing a backoff against that advice is
// the caller's job, since only the caller knows its own deadline.
func (p *Provider) Completion(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	ctx, op := p.o11y.Begin(ctx)
	defer op.End()

	startTime := time.Now()
	defer func() {
		p.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	params, err := p.params(req, op)
	if err != nil {
		p.errorCounter.Add(ctx, 1)

		return nil, op.Error(err, "building request")
	}

	resp, err := p.provider.Completion(ctx, params)
	if err != nil {
		p.errorCounter.Add(ctx, 1)

		return nil, op.Error(bridge.NormalizeError(err), "completing request")
	}

	p.requestCounter.Add(ctx, 1)

	out := bridge.Response(resp)
	if out.Usage != nil {
		op.Set("llm.tokens.total", out.Usage.TotalTokens)
	}
	op.Set("llm.stop_reason", string(out.StopReason))

	return out, nil
}

// Stream implements llm.Provider.
//
// The span and the latency measurement cover the whole stream rather than the
// call that starts it, which is why they are ended by the returned stream's
// finish hook instead of a defer here. A consumer that abandons the stream
// without closing it leaves both open; llm.Stream documents Close as mandatory
// for exactly this reason.
func (p *Provider) Stream(ctx context.Context, req *llm.CompletionRequest) (llm.Stream, error) {
	ctx, op := p.o11y.Begin(ctx)

	params, err := p.params(req, op)
	if err != nil {
		defer op.End()
		p.errorCounter.Add(ctx, 1)

		return nil, op.Error(err, "building request")
	}

	// OpenAI omits token accounting from a stream unless asked, so a streamed
	// llm.EventDone would otherwise carry no Usage at all. Anthropic reports it
	// unconditionally and ignores this option, which is why it is set here
	// rather than in the shared translation.
	params.StreamOptions = &anyllm.StreamOptions{IncludeUsage: true}

	startTime := time.Now()
	p.requestCounter.Add(ctx, 1)

	streamCtx, cancel := context.WithCancel(ctx)
	chunks, errs := p.provider.CompletionStream(streamCtx, params)

	return bridge.Stream(chunks, errs, func(streamErr error) {
		cancel()
		p.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))

		if streamErr != nil {
			p.errorCounter.Add(ctx, 1)
			op.Acknowledge(streamErr, "streaming completion")
		}

		op.End()
	}), nil
}

// params resolves the model and translates the request, recording what was
// asked for on the operation either way.
func (p *Provider) params(req *llm.CompletionRequest, op observability.Operation) (anyllm.CompletionParams, error) {
	model := ""
	messageCount := 0
	if req != nil {
		model = req.Model
		messageCount = len(req.Messages)
	}

	if model == "" {
		model = p.defaultModel
	}
	if model == "" {
		model = fallbackModel
	}

	op.Set("llm.model", model).Set("llm.message_count", messageCount)

	return bridge.Params(req, model)
}
