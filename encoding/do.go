package encoding

import (
	"github.com/primandproper/platform-go/v9/observability"

	"github.com/samber/do/v2"
)

// RegisterServerEncoderDecoder registers a ContentType and ServerEncoderDecoder with the injector.
func RegisterServerEncoderDecoder(i do.Injector) {
	do.Provide[ContentType](i, func(i do.Injector) (ContentType, error) {
		return NewContentType(do.MustInvoke[Config](i))
	})
	do.Provide[ServerEncoderDecoder](i, func(i do.Injector) (ServerEncoderDecoder, error) {
		pillars, err := observability.InvokePillars(i)
		if err != nil {
			return nil, err
		}

		return NewServerEncoderDecoder(
			do.MustInvoke[ContentType](i),
			WithLogger(pillars.Logger),
			WithTracerProvider(pillars.TracerProvider),
		), nil
	})
}
