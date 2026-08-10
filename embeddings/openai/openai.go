package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/primandproper/platform-go/v10/embeddings"
	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
)

const (
	defaultBaseURL = "https://api.openai.com"
	defaultModel   = "text-embedding-3-small"
	// name scopes this package's spans, logs, and metrics. It is qualified with
	// the component because llm/openai instruments the same vendor, and the two
	// would otherwise share an instrumentation scope.
	name = "openai_embeddings"
	// providerName is what the Provider field of a returned Embedding reports:
	// the vendor rather than the component, since it is stored alongside the
	// vector and read back to reason about the model.
	providerName = "openai"
)

type embedder struct {
	o11y           observability.Observer
	client         *http.Client
	cfg            *Config
	requestCounter metrics.Int64Counter
	errorCounter   metrics.Int64Counter
	latencyHist    metrics.Float64Histogram
}

// NewEmbedder creates a new OpenAI-backed embeddings provider.
func NewEmbedder(ctx context.Context, cfg *Config, opts ...Option) (embeddings.Embedder, error) {
	if cfg == nil {
		return nil, errors.New("openai embeddings config is required")
	}

	o := newOptions(opts)
	logger := logging.EnsureLogger(o.logger)

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating openai embeddings config")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = embeddings.DefaultRequestTimeout
	}
	client := &http.Client{Timeout: timeout}

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

	return &embedder{
		o11y:           observability.NewObserver(name, logger, o.tracerProvider),
		client:         client,
		cfg:            cfg,
		requestCounter: requestCounter,
		errorCounter:   errorCounter,
		latencyHist:    latencyHist,
	}, nil
}

type embeddingRequest struct {
	Model          string   `json:"model"`
	EncodingFormat string   `json:"encoding_format"`
	Input          []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// GenerateEmbeddings implements embeddings.Embedder.
//
// Every input in a call must resolve to the same model, because OpenAI embeds
// one batch against one model; a batch spanning two models is rejected rather
// than silently split, which would make the round-trip count depend on the
// caller's ordering.
//
// Rate limiting: this method does not retry. A non-200 response (including 429 Too Many
// Requests) is surfaced to the caller as an error carrying the status code; it is not
// retried or backed off. Callers that want retry/backoff should wrap this call themselves
// (e.g. with the platform's retry package).
func (e *embedder) GenerateEmbeddings(ctx context.Context, inputs []*embeddings.Input) (_ []*embeddings.Embedding, err error) {
	ctx, op := e.o11y.Begin(ctx)
	defer op.End()

	if len(inputs) == 0 {
		return []*embeddings.Embedding{}, nil
	}

	// Instrumented here rather than in GenerateEmbedding, which delegates to
	// this method — counting both would double every single-input call.
	e.requestCounter.Add(ctx, 1)
	startTime := time.Now()
	defer func() {
		e.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
		if err != nil {
			e.errorCounter.Add(ctx, 1)
		}
	}()

	texts := make([]string, len(inputs))
	var model string
	for i, input := range inputs {
		if input == nil {
			return nil, embeddings.ErrNilInput
		}

		texts[i] = input.Content

		m := input.Model
		if m == "" {
			m = e.cfg.DefaultModel
		}
		if m == "" {
			m = defaultModel
		}

		if i == 0 {
			model = m
		} else if m != model {
			return nil, op.Error(errors.Newf("batch spans models %q and %q", model, m), "mixed models in one batch")
		}
	}

	op.Set(keys.EmbeddingModelKey, model).Set(keys.LengthKey, len(inputs))

	baseURL := e.cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	reqBody := embeddingRequest{
		Input:          texts,
		Model:          model,
		EncodingFormat: "float",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, op.Error(err, "marshaling request")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/embeddings", baseURL), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, op.Error(err, "building request")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", e.cfg.APIKey))

	resp, err := e.client.Do(req) //nolint:gosec // G704: URL is constructed from trusted config
	if err != nil {
		return nil, op.Error(err, "executing request")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			op.Acknowledge(closeErr, "closing response body")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, errors.Wrap(readErr, "reading openai error response body")
		}
		err = errors.Errorf("openai embedding API returned status %d: %s", resp.StatusCode, string(body))
		return nil, op.Error(err, "unexpected status code")
	}

	var embResp embeddingResponse
	if err = json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, op.Error(err, "decoding response")
	}

	if len(embResp.Data) != len(inputs) {
		return nil, op.Error(
			errors.Newf("openai returned %d embeddings for %d inputs", len(embResp.Data), len(inputs)),
			"mismatched response length",
		)
	}

	now := time.Now()
	out := make([]*embeddings.Embedding, len(inputs))
	for i, d := range embResp.Data {
		vector := toFloat32(d.Embedding)
		out[i] = &embeddings.Embedding{
			Vector:      vector,
			SourceText:  texts[i],
			Model:       model,
			Provider:    providerName,
			Dimensions:  len(vector),
			GeneratedAt: now,
		}
	}

	op.Set("embedding.dimensions", out[0].Dimensions)

	return out, nil
}

// GenerateEmbedding implements embeddings.Embedder by embedding one input.
//
// It is a thin wrapper over GenerateEmbeddings: OpenAI's API takes an array,
// so one input is simply a batch of one.
func (e *embedder) GenerateEmbedding(ctx context.Context, input *embeddings.Input) (*embeddings.Embedding, error) {
	out, err := e.GenerateEmbeddings(ctx, []*embeddings.Input{input})
	if err != nil {
		return nil, err
	}

	return out[0], nil
}

func toFloat32(f64 []float64) []float32 {
	out := make([]float32, len(f64))
	for i, v := range f64 {
		out[i] = float32(v)
	}
	return out
}
