/*
Package uploads provides an object storage abstraction for saving and reading files, with
implementations backed by S3, GCS, Cloudflare R2, Backblaze B2, the local filesystem, and an
in-memory provider (see the objectstorage subpackage).

It moves bytes and hands back a key. What the object is — who uploaded it, into which tenant, its
content type, its size, what it belongs to — is a row rather than a bucket property, and the
registry subpackage is where that row lives. Reach for it wherever an object is not
public-by-key: the question "may this caller read this object" is answered from the row's owner
and scope.
*/
package uploads
