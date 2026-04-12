package qrcodes

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/boombuler/barcode"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewBuilder(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		b := NewBuilder("test-issuer", nil, nil)
		test.NotNil(t, b)
	})
}

func Test_builder_BuildQRCode(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		b := NewBuilder("test-issuer", nil, nil)

		actual, err := b.BuildQRCode(ctx, "username", "two-factor-secret")
		must.NoError(t, err)
		test.NotEq(t, "", actual)
	})

	T.Run("with content exceeding QR capacity", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		b := NewBuilder("test-issuer", nil, nil)

		// A username longer than the maximum QR code capacity forces qr.Encode to fail.
		actual, err := b.BuildQRCode(ctx, strings.Repeat("a", 4000), "two-factor-secret")
		test.EqOp(t, "", actual)
		test.Error(t, err)
	})

	T.Run("with scale error", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		b := NewBuilder("test-issuer", nil, nil).(*builder)
		b.scale = func(barcode.Barcode, int, int) (barcode.Barcode, error) {
			return nil, fmt.Errorf("scale error")
		}

		actual, err := b.BuildQRCode(ctx, "username", "two-factor-secret")
		test.EqOp(t, "", actual)
		test.Error(t, err)
	})

	T.Run("with png encode error", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()

		b := NewBuilder("test-issuer", nil, nil).(*builder)
		b.pngEncode = func(*bytes.Buffer, barcode.Barcode) error {
			return fmt.Errorf("png encode error")
		}

		actual, err := b.BuildQRCode(ctx, "username", "two-factor-secret")
		test.EqOp(t, "", actual)
		test.Error(t, err)
	})
}
