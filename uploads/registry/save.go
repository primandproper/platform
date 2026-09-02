package registry

import (
	"context"
	"io"

	"github.com/primandproper/platform-go/v14/uploads"
)

// StoreAndRecord writes the bytes and then registers what was written, filling
// in the object's Size from what actually went past.
//
// It is the convenience, not the contract. Storing and registering stay
// separately callable, and they have to: bytes that arrived through a signed URL
// were stored by the client rather than by this process, and a consumer
// migrating an existing bucket registers objects nothing here ever wrote. So
// this is a free function over the two seams rather than a method on either, and
// nothing in the Store or the UploadManager knows it exists.
//
// Size comes from the copy rather than from the caller, because the caller's
// number is a claim. A Content-Length header is whatever the client sent, and a
// quota, a bill, or a storage report read off claimed sizes is one that does not
// hold. Counting the bytes as they go past is the only number that is about what
// is in the bucket.
//
// object.Key must be set — it is where the bytes go — and object.ContentType, if
// set, is what the object is stored with, so the row and the stored object agree
// about the type. Everything else about the object is the caller's: the owner,
// the scope, the subject it hangs off.
//
// The order is deliberate and it is the one that fails safe. The bytes go first,
// so a failure to register leaves an object with no row — invisible to every
// read, and exactly what an orphan sweep is later written to find. Registering
// first would leave a row pointing at bytes that are not there, which every read
// reports as an object the caller may have and every fetch then fails to
// deliver. Neither is free; only one of them lies to a reader.
func StoreAndRecord(
	ctx context.Context,
	manager uploads.UploadManager,
	store Store,
	object *Object,
	r io.Reader,
	opts ...uploads.SaveOption,
) error {
	switch {
	case manager == nil:
		return ErrNilUploadManager
	case store == nil:
		return ErrNilStore
	case object == nil:
		return ErrNilObject
	case r == nil:
		return ErrNilReader
	}

	// The content type is stated to the provider as well as recorded, so the
	// stored object and its row agree. An empty one is left alone: the providers
	// sniff it from the content, and naming it explicitly as "" would replace a
	// sniffed answer with none.
	if object.ContentType != "" {
		opts = append(opts, uploads.WithContentType(object.ContentType))
	}

	counted := &countingReader{r: r}

	if err := manager.Save(ctx, object.Key, counted, opts...); err != nil {
		return err
	}

	object.Size = counted.n

	return store.RecordObject(ctx, object)
}

// countingReader counts the bytes that pass through it.
//
// It wraps the caller's reader rather than tee-ing into a buffer, so an upload
// of any size costs one int64. It is not safe for concurrent use, and does not
// need to be: an UploadManager.Save reads its reader from one goroutine.
type countingReader struct {
	r io.Reader
	n int64
}

var _ io.Reader = (*countingReader)(nil)

func (c *countingReader) Read(p []byte) (int, error) {
	read, err := c.r.Read(p)
	c.n += int64(read)

	return read, err
}
