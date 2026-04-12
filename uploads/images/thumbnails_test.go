package images

import (
	"os"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func Test_newThumbnailer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		for _, ct := range []string{imagePNG, imageJPEG, imageGIF} {
			x, err := newThumbnailer(ct)
			test.NoError(t, err)
			test.NotNil(t, x)
		}
	})

	T.Run("invalid content type", func(t *testing.T) {
		t.Parallel()

		x, err := newThumbnailer(t.Name())
		test.Error(t, err)
		test.Nil(t, x)
	})
}

func Test_preprocess(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		imgBytes := buildPNGBytes(t).Bytes()
		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        imgBytes,
			Size:        len(imgBytes),
		}

		img, err := preprocess(i, 128, 128)
		test.NoError(t, err)
		test.NotNil(t, img)
	})

	T.Run("with invalid content", func(t *testing.T) {
		t.Parallel()

		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        []byte(t.Name()),
			Size:        1024,
		}

		img, err := preprocess(i, 128, 128)
		test.Error(t, err)
		test.Nil(t, img)
	})
}

func Test_jpegThumbnailer_Thumbnail(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		imgBytes := buildJPEGBytes(t).Bytes()
		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        imgBytes,
			Size:        len(imgBytes),
		}

		tempFile, err := os.CreateTemp("", "")
		must.NoError(t, err)

		actual, err := (&jpegThumbnailer{}).Thumbnail(i, 128, 128, tempFile.Name())
		test.NoError(t, err)
		test.NotNil(t, actual)

		must.NoError(t, os.Remove(tempFile.Name()))
	})

	T.Run("with invalid content", func(t *testing.T) {
		t.Parallel()

		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        []byte(t.Name()),
			Size:        1024,
		}

		tempFile, err := os.CreateTemp("", "")
		must.NoError(t, err)

		actual, err := (&jpegThumbnailer{}).Thumbnail(i, 128, 128, tempFile.Name())
		test.Error(t, err)
		test.Nil(t, actual)

		must.NoError(t, os.Remove(tempFile.Name()))
	})
}

func Test_pngThumbnailer_Thumbnail(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		imgBytes := buildPNGBytes(t).Bytes()
		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        imgBytes,
			Size:        len(imgBytes),
		}

		tempFile, err := os.CreateTemp("", "")
		must.NoError(t, err)

		actual, err := (&pngThumbnailer{}).Thumbnail(i, 128, 128, tempFile.Name())
		test.NoError(t, err)
		test.NotNil(t, actual)

		must.NoError(t, os.Remove(tempFile.Name()))
	})

	T.Run("with invalid content", func(t *testing.T) {
		t.Parallel()

		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        []byte(t.Name()),
			Size:        1024,
		}

		tempFile, err := os.CreateTemp("", "")
		must.NoError(t, err)

		actual, err := (&pngThumbnailer{}).Thumbnail(i, 128, 128, tempFile.Name())
		test.Error(t, err)
		test.Nil(t, actual)

		must.NoError(t, os.Remove(tempFile.Name()))
	})
}

func Test_gifThumbnailer_Thumbnail(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		imgBytes := buildGIFBytes(t).Bytes()
		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        imgBytes,
			Size:        len(imgBytes),
		}

		tempFile, err := os.CreateTemp("", "")
		must.NoError(t, err)

		actual, err := (&gifThumbnailer{}).Thumbnail(i, 128, 128, tempFile.Name())
		test.NoError(t, err)
		test.NotNil(t, actual)

		must.NoError(t, os.Remove(tempFile.Name()))
	})

	T.Run("with invalid content", func(t *testing.T) {
		t.Parallel()

		i := &Upload{
			Filename:    "whatever.png",
			ContentType: imagePNG,
			Data:        []byte(t.Name()),
			Size:        1024,
		}

		tempFile, err := os.CreateTemp("", "")
		must.NoError(t, err)

		actual, err := (&gifThumbnailer{}).Thumbnail(i, 128, 128, tempFile.Name())
		test.Error(t, err)
		test.Nil(t, actual)

		must.NoError(t, os.Remove(tempFile.Name()))
	})
}
