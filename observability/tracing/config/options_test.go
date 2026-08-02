package tracingcfg

import (
	"testing"

	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"

	"github.com/shoenig/test"
)

func TestOptions(T *testing.T) {
	T.Parallel()

	T.Run("each option sets the field it names", func(t *testing.T) {
		t.Parallel()

		logger := loggingnoop.NewLogger()

		o := newOptions([]Option{
			WithLogger(logger),
		})

		test.Eq(t, logger, o.logger)
	})

	T.Run("nil options are ignored", func(t *testing.T) {
		t.Parallel()

		o := newOptions([]Option{nil})

		test.Nil(t, o.logger)
	})
}
