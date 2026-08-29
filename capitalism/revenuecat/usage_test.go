package revenuecat

import (
	"errors"
	"testing"

	"github.com/shoenig/test"
)

func TestErrUsageReportingUnsupported(T *testing.T) {
	T.Parallel()

	T.Run("is distinct from the outbound purchase refusal", func(t *testing.T) {
		t.Parallel()

		// Two refusals with two reasons. Collapsing them — by aliasing one onto the
		// other, or by pointing capitalismcfg's usage reporter back at the outbound
		// one — tells a deployment that flushed usage here that RevenueCat has no
		// server-side purchase API, which is true and is not why the flush was
		// refused.
		test.False(t, errors.Is(ErrUsageReportingUnsupported, ErrOutboundUnsupported))
		test.False(t, errors.Is(ErrOutboundUnsupported, ErrUsageReportingUnsupported))
	})

	T.Run("says why RevenueCat does not meter", func(t *testing.T) {
		t.Parallel()

		// The message is the whole of what a consumer gets — there is no adapter to
		// read a doc comment off — so it has to name the model rather than only
		// report a refusal.
		test.StrContains(t, ErrUsageReportingUnsupported.Error(), "metered consumption")
	})
}
