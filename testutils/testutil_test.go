package testutils

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestBuildArbitraryImage(T *testing.T) {
	T.Parallel()

	T.Run("returns image with correct dimensions", func(t *testing.T) {
		t.Parallel()
		img := BuildArbitraryImage(10)
		must.NotNil(t, img)
		bounds := img.Bounds()
		test.EqOp(t, 10, bounds.Dx())
		test.EqOp(t, 10, bounds.Dy())
	})

	T.Run("handles size 1", func(t *testing.T) {
		t.Parallel()
		img := BuildArbitraryImage(1)
		must.NotNil(t, img)
		test.EqOp(t, 1, img.Bounds().Dx())
	})
}

func TestBuildArbitraryImagePNGBytes(T *testing.T) {
	T.Parallel()

	T.Run("returns valid PNG bytes", func(t *testing.T) {
		t.Parallel()
		img, data := BuildArbitraryImagePNGBytes(5)
		must.NotNil(t, img)
		test.SliceNotEmpty(t, data)
		// PNG magic bytes
		test.EqOp(t, byte(0x89), data[0])
		test.EqOp(t, byte('P'), data[1])
		test.EqOp(t, byte('N'), data[2])
		test.EqOp(t, byte('G'), data[3])
	})
}

func TestBuildTestRequest(T *testing.T) {
	T.Parallel()

	T.Run("returns valid request", func(t *testing.T) {
		t.Parallel()
		req := BuildTestRequest(t)
		must.NotNil(t, req)
		test.NotNil(t, req.Context())
	})
}
