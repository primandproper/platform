package encoding

import (
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/tracing"

	"github.com/samber/do/v2"
)

// RegisterServerEncoderDecoder registers a ContentType and ServerEncoderDecoder with the injector.
func RegisterServerEncoderDecoder(i do.Injector) {
	do.Provide[ContentType](i, func(i do.Injector) (ContentType, error) {
		return NewContentType(do.MustInvoke[Config](i)), nil
	})
	do.Provide[ServerEncoderDecoder](i, func(i do.Injector) (ServerEncoderDecoder, error) {
		return NewServerEncoderDecoder(
			do.MustInvoke[ContentType](i),
			WithLogger(do.MustInvoke[logging.Logger](i)),
			WithTracerProvider(do.MustInvoke[tracing.TracerProvider](i)),
		), nil
	})
}
