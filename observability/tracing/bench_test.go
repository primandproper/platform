package tracing

import (
	"context"
	"net/http"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// These price the primitives observability.Observer.Begin is assembled from, so
// that a number measured there can be attributed here rather than guessed at.

// BenchmarkGetCallerName is the cost of naming a span after the function that
// opened it.
//
// The cached row is the one that matters: a call site's program counter is
// stable, so every call after the first for a given site hits the memoized
// name. It is allocation-free by design — TestGetCallerName_allocations asserts
// that — but not free, because runtime.Callers walks the stack before the cache
// can be consulted at all. That residual walk is the whole cost of naming spans
// by reflection rather than by literal, and it is what an explicit-name API
// would buy back.
//
// The uncached row prices a cold call site, including the allocating
// runtime.Func.Name call the cache exists to amortize. A process pays it once
// per instrumented method, ever.
func BenchmarkGetCallerName(b *testing.B) {
	b.Run("cached", func(b *testing.B) {
		// Warm the entry for this call site so the loop measures hits.
		stringSink = getCallerNameStub()

		for b.Loop() {
			stringSink = getCallerNameStub()
		}
	})

	b.Run("uncached", func(b *testing.B) {
		for b.Loop() {
			b.StopTimer()
			callerNameCache.Clear()
			b.StartTimer()

			stringSink = getCallerNameStub()
		}
	})
}

// getCallerNameStub stands in for StartSpan: the direct caller of
// GetCallerName, which is the frame depth callerSkip is calibrated for.
//
//go:noinline
func getCallerNameStub() string {
	return GetCallerName()
}

// BenchmarkStartSpan prices span creation through the wrapper every component
// uses, against both a noop provider and a recording one.
//
// StartSpan versus StartCustomSpan is the same comparison
// BenchmarkObserver_BeginVersusBeginCustom makes one layer up: the only
// difference between them is whether the name came off the stack or off the
// caller.
func BenchmarkStartSpan(b *testing.B) {
	ctx := b.Context()

	sdk := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	b.Cleanup(func() { _ = sdk.Shutdown(context.Background()) })

	providers := []struct {
		provider Provider
		name     string
	}{
		{name: "noopProvider", provider: nil},
		{name: "recordingProvider", provider: sdk},
	}

	for i := range providers {
		p := &providers[i]

		tracer := NewNamedTracer(p.provider, "bench")

		b.Run(p.name+"/StartSpan", func(b *testing.B) {
			for b.Loop() {
				c, span := tracer.StartSpan(ctx)
				ctxSink = c

				span.End()
			}
		})

		b.Run(p.name+"/StartCustomSpan", func(b *testing.B) {
			for b.Loop() {
				c, span := tracer.StartCustomSpan(ctx, "bench.staticName")
				ctxSink = c

				span.End()
			}
		})
	}
}

// BenchmarkAttachToSpan prices attaching one value, which is what every
// Operation.Set does to the span half of its work.
//
// The noop row is the one worth reading: AttachToSpan gates on IsRecording
// precisely so a component that traces nowhere does not pay to build attributes
// nobody will read, and this is the measurement that says the guard works. The
// recording row is what the same call costs when the span will actually keep it.
func BenchmarkAttachToSpan(b *testing.B) {
	ctx := b.Context()

	sdk := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	b.Cleanup(func() { _ = sdk.Shutdown(context.Background()) })

	spans := []struct {
		span trace.Span
		name string
	}{}

	_, noopSpan := NewNamedTracer(nil, "bench").StartCustomSpan(ctx, "bench")
	spans = append(spans, struct {
		span trace.Span
		name string
	}{name: "noopSpan", span: noopSpan})

	_, recordingSpan := NewNamedTracer(sdk, "bench").StartCustomSpan(ctx, "bench")
	spans = append(spans, struct {
		span trace.Span
		name string
	}{name: "recordingSpan", span: recordingSpan})

	for i := range spans {
		s := &spans[i]

		b.Run(s.name+"/string", func(b *testing.B) {
			for b.Loop() {
				AttachToSpan(s.span, "key", "value")
			}
		})

		b.Run(s.name+"/int", func(b *testing.B) {
			for b.Loop() {
				AttachToSpan(s.span, "key", 42)
			}
		})

		// An arbitrary type falls through keyValueForValue's type switch to the
		// reflective/stringifying branch, which is the expensive one.
		b.Run(s.name+"/struct", func(b *testing.B) {
			value := struct {
				Name string
				ID   int
			}{Name: "bench", ID: 42}

			for b.Loop() {
				AttachToSpan(s.span, "key", value)
			}
		})
	}
}

// BenchmarkAttachRequestToSpan prices the per-request attacher, which
// stringifies a URL, parses a user agent, and formats and redacts every header,
// making it the heaviest of the attachers by a wide margin.
//
// The two rows have to stay far apart. Attaching a request is worth its cost
// only when something will read the result, so the composite attachers check
// whether the span is recording once, before doing any of that work, rather
// than relying on AttachToSpan to discard the values afterwards. If the noop row
// ever approaches the recording one, that guard has been lost — and the cost
// would land on every request of every service that samples, which is all of
// them.
func BenchmarkAttachRequestToSpan(b *testing.B) {
	ctx := b.Context()

	sdk := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	b.Cleanup(func() { _ = sdk.Shutdown(context.Background()) })

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/charges?limit=50", http.NoBody)
	if err != nil {
		b.Fatal(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer some-token-value")
	req.Header.Set("X-Request-Id", "req_01HZY0000000000000")
	req.Header.Set("User-Agent", "bench/1.0")

	_, noopSpan := NewNamedTracer(nil, "bench").StartCustomSpan(ctx, "bench")
	_, recordingSpan := NewNamedTracer(sdk, "bench").StartCustomSpan(ctx, "bench")

	b.Run("noopSpan", func(b *testing.B) {
		for b.Loop() {
			AttachRequestToSpan(noopSpan, req)
		}
	})

	b.Run("recordingSpan", func(b *testing.B) {
		for b.Loop() {
			AttachRequestToSpan(recordingSpan, req)
		}
	})
}

var (
	stringSink string
	ctxSink    context.Context
)
