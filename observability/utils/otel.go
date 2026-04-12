package o11yutils

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func MustOtelResource(ctx context.Context, serviceName string) *resource.Resource {
	options := []resource.Option{
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithOSType(),
	}

	if serviceName != "" {
		options = append(options,
			resource.WithAttributes(
				attribute.KeyValue{
					Key:   semconv.ServiceNameKey,
					Value: attribute.StringValue(serviceName),
				},
			),
		)
	}

	res, err := resource.New(ctx, options...)
	if err != nil {
		panic(err)
	}

	return res
}
