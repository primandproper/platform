package objectstorage

import (
	"testing"

	"github.com/shoenig/test"
	"gocloud.dev/blob/memblob"
)

func TestProvideUploadManager(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		u := &Uploader{
			bucket: memblob.OpenBucket(&memblob.Options{}),
		}

		result := ProvideUploadManager(u)
		test.NotNil(t, result)
		test.True(t, u == result)
	})
}
