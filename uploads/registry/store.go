package registry

import (
	"context"

	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Store is the registry: the rows that say what the objects in storage are.
//
// Every read is scoped and there is no unscoped variant of any of them. That is
// the point rather than a convenience: the caller who reaches for an unscoped
// read is the caller who has not thought about tenancy, and the way to make
// that unreachable is not to ship one. An application with a single tenant
// passes tenancy.Global() everywhere and gets exactly the behavior it would
// have had without the column.
//
// Nothing here touches bytes. The Store writes and reads rows; uploads.UploadManager
// writes and reads objects. StoreAndRecord is the convenience that does both,
// and it is a free function rather than a method precisely so that storing and
// registering stay separately callable — a consumer whose bytes arrived through
// a signed URL registers what somebody else stored.
type Store interface {
	// RecordObject writes the row for an object in storage, filling in the
	// object's ID when it has none and its CreatedAt from the row.
	//
	// The key must be free within the scope, archived rows included: a key
	// already registered is ErrObjectKeyTaken. It runs in its own transaction —
	// the collision check, the insert and the creation-time read-back are one
	// unit — so a consumer that needs the row to commit with a row of their own
	// writes theirs after this returns and compensates if it fails, or registers
	// first and stores the bytes after.
	RecordObject(ctx context.Context, object *Object) error

	// GetObject reads one of the scope's objects by row id. An archived object
	// reads as absent.
	GetObject(ctx context.Context, scope tenancy.Scope, objectID string) (*Object, error)

	// GetObjectByKey reads one of the scope's objects by the key its bytes live
	// at, which is what a request holding a URL path rather than a row id runs.
	// An archived object reads as absent.
	GetObjectByKey(ctx context.Context, scope tenancy.Scope, key string) (*Object, error)

	// ListObjects pages the scope's objects, in the direction the filter names.
	ListObjects(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Object], error)

	// ListObjectsByOwner pages one owner's objects within the scope.
	ListObjectsByOwner(ctx context.Context, scope tenancy.Scope, ownerID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Object], error)

	// ListObjectsBySubject pages the objects attached to one thing within the
	// scope. The subject must name something — see ErrUnattachedSubject.
	ListObjectsBySubject(ctx context.Context, scope tenancy.Scope, subject Subject, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Object], error)

	// ArchiveObject soft-deletes the row.
	//
	// Metadata-only, and deliberately: the object stays in the bucket. Whether
	// and when the bytes go is the consumer's retention policy, because the
	// registry is not the thing that knows whether a receipt is still needed
	// for tax purposes. Archiving a row that is already archived, or one in
	// another scope, is ErrObjectNotFound.
	ArchiveObject(ctx context.Context, scope tenancy.Scope, objectID string) error
}
