package resources

import (
	"database/sql/driver"
	"reflect"

	"github.com/primandproper/platform-go/v12/tenancy"
)

// role is what a column is beyond a place to put a value: the primary key, the
// tenancy scope, the owner. Everything else is plain data.
type role uint8

const (
	roleData role = iota
	roleID
	roleScope
	roleOwner
)

// Gate is what an owner column controls.
type Gate uint8

const (
	// OwnerWrites means anyone who can see the row may read it, and only its
	// owner may update or archive it. It is the default because it is what most
	// owned rows are: a comment on a public recipe, a review, a rating.
	OwnerWrites Gate = iota
	// OwnerReadsAndWrites means the owner is the only one who may see the row at
	// all — a draft, a private note, a saved search.
	OwnerReadsAndWrites
)

// Column is one column of a resource's table, bound to one field of its row
// type.
//
// The binding is an accessor returning a pointer to the field, and that single
// accessor serves every direction: database/sql dereferences a pointer when
// binding a parameter and allocates through one when scanning, so the same
// func(*T) *V both writes the column and reads it. A nullable column is one
// whose field is itself a pointer, reached as func(*T) **V, and the same two
// rules apply one level down — a nil field binds NULL, and a NULL scan sets the
// field back to nil.
type Column[T any] struct {
	// ref returns a pointer to the field, type-erased. It is what is handed to
	// both Scan and the argument list.
	ref func(*T) any
	// str reads the column as a string, and is set only for the columns whose
	// values this package itself has to read: the id, the owner. It is a
	// separate function rather than a type assertion on ref's result because
	// the assertion would be unchecked at the point it matters most.
	str func(*T) string
	// valueType is the field's type, kept so a Match against this column can be
	// checked before it reaches a driver. See Column.accepts.
	valueType reflect.Type
	// name is the column name, interpolated into statement text and therefore
	// restricted rather than escaped.
	name string
	role role
	gate Gate
	// nullable is derived from the accessor's type rather than declared. See
	// Field.
	nullable bool
	// immutable excludes the column from UPDATE. See Column.Immutable.
	immutable bool
}

// Name returns the column's name.
func (c Column[T]) Name() string { return c.name }

// Field declares a plain data column bound to one field of the row type.
//
// Nullability is inferred: a V that is itself a pointer type is a column that
// may be NULL. Nothing declares it, because the field's type already says so
// and a second statement of the same fact is one that can disagree with the
// first.
//
// The three conventional timestamp columns — created_at, last_updated_at,
// archived_at — are declared with Field like any other, and are recognized by
// name. Their presence is what decides behavior, exactly as it does in
// database/querygen: a resource with no archived_at column has no Archive
// method to call, rather than one that silently does nothing.
func Field[T, V any](name string, ref func(*T) *V) Column[T] {
	return Column[T]{
		name:      name,
		role:      roleData,
		valueType: reflect.TypeFor[V](),
		nullable:  reflect.TypeFor[V]().Kind() == reflect.Pointer,
		ref:       func(row *T) any { return ref(row) },
	}
}

// ID declares the primary key column, which is always named id and always a
// string.
//
// It is a separate constructor rather than a Field whose name happens to be
// "id" because this package reads the value — an archive names it, a hook
// carries it — and a key it could only reach through an unchecked type
// assertion is a key it should not be reaching at all.
//
// The id doubles as the pagination cursor, so it has to sort by creation time:
// an xid or a ULID, not a serial and not a UUIDv4. A keyset walk over an id
// that does not sort that way pages in an order nobody asked for.
func ID[T any](ref func(*T) *string) Column[T] {
	return Column[T]{
		name:      "id",
		role:      roleID,
		valueType: reflect.TypeFor[string](),
		ref:       func(row *T) any { return ref(row) },
		str:       func(row *T) string { return *ref(row) },
	}
}

// Owner declares the column carrying the row's author, and what it gates.
//
// An owner is not a tenancy scope. It says who wrote the row, which is a
// question about permission to change it; a scope says whose data it is, which
// is a question about who may see it at all. A resource may have both, one, or
// neither, and conflating them is how a store ends up unable to answer a read
// its application actually asks — see Scope.
func Owner[T any](name string, ref func(*T) *string, gate Gate) Column[T] {
	return Column[T]{
		name:      name,
		role:      roleOwner,
		gate:      gate,
		valueType: reflect.TypeFor[string](),
		ref:       func(row *T) any { return ref(row) },
		str:       func(row *T) string { return *ref(row) },
	}
}

// Scope declares the column carrying the row's tenancy scope.
//
// The field is a tenancy.Scope rather than a string, so it is bound as one: an
// unset scope is refused by the driver rather than read as the global one. That
// is the whole reason the column's type is not string — see tenancy.Scope.Value.
//
// A resource whose table has no such column declares Unscoped on the Definition
// instead. It does not declare nothing.
func Scope[T any](name string, ref func(*T) *tenancy.Scope) Column[T] {
	return Column[T]{
		name:      name,
		role:      roleScope,
		valueType: reflect.TypeFor[tenancy.Scope](),
		ref:       func(row *T) any { return ref(row) },
	}
}

// Immutable marks a column as one an update never assigns.
//
// It is for the facts a row is created with and never changes again: what it
// refers to, who wrote it, the parent it hangs off. They are still inserted and
// still read; they are excluded from UPDATE only, which is what makes "change
// this comment's content" unable to also change which recipe it is attached to.
func (c Column[T]) Immutable() Column[T] {
	c.immutable = true

	return c
}

// accepts reports whether a Match's value is a plausible value for this column.
//
// Match.Value is an `any`, and it is one deliberately: making it typed means
// Column[T, V], and a Column carrying its value type as a parameter cannot sit
// in the []Column[T] a declaration is. The type argument would have to be spelled
// at every column of every declaration to buy a check on the handful of values a
// keyed read binds — so the type stays out of the signature, and the check
// happens here instead, at the boundary where the value arrives.
//
// The check is by kind rather than by identity, and that is the part worth
// stating. A column whose field is a named string type — an enum wrapper, a
// typed id — is a text column as far as the driver is concerned, and a caller
// matching it against a plain string is not making a mistake. What this refuses
// is a value of the wrong shape entirely: an int against a text column, a string
// against a timestamp. Those are the ones that reach the server as a type error
// naming a placeholder number, from a call site the number does not identify.
//
// A driver.Valuer is admitted whatever its kind, because what it binds is
// whatever it decides to return and this package is not the thing that knows.
func (c Column[T]) accepts(value any) bool {
	target := c.valueType
	if target == nil {
		return true
	}

	if c.nullable {
		target = target.Elem()
	}

	if value == nil {
		return c.nullable
	}

	if _, ok := value.(driver.Valuer); ok {
		return true
	}

	given := reflect.TypeOf(value)
	if given.Kind() == reflect.Pointer && target.Kind() != reflect.Pointer {
		given = given.Elem()
	}

	return given.AssignableTo(target) || given.Kind() == target.Kind()
}
