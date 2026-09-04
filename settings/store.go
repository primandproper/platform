package settings

import (
	"context"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Store is the whole of what this package persists: the catalog and the
// answers stored against it.
//
// It is two interfaces because they have two callers. A DefinitionStore is
// reached by whatever administers a deployment — a migration, an admin console,
// a seeding job — and a ValueStore is reached on the request path, by the
// handler saving somebody's notification preference and by the code that reads
// it back. Splitting them is what lets a component depend on the half it uses;
// Store is here for the wiring that provides both.
type Store interface {
	DefinitionStore
	ValueStore
}

// DefinitionStore is the catalog: what settings exist, what kind of value each
// holds, and what each falls back to.
//
// Every method takes a tenancy.Scope, and a deployment with one catalog passes
// tenancy.Global() to all of them. There is deliberately no unscoped variant of
// any of these — see the tenancy package, and the settings package
// documentation on why a definition and the values against it share a scope.
type DefinitionStore interface {
	// CreateDefinition adds a setting to the catalog and returns it as
	// stored, with the id it was minted under and the creation time the
	// database assigned.
	//
	// It refuses a name already defined in this scope with
	// ErrDefinitionNameTaken, a default the setting would not admit, and an
	// enumeration holding an empty or repeated value.
	CreateDefinition(ctx context.Context, scope tenancy.Scope, definition *Definition) (*Definition, error)

	// CreateDefinitionTx is CreateDefinition inside the caller's transaction, so
	// the definition commits with whatever the caller writes beside it. A nil q
	// is an error wrapping ErrNilExecutor.
	//
	// It exists because a row in a consumer's schema is rarely written alone. An
	// audit entry naming who defined the setting and a data change event on an
	// outbox somebody fans out are the ordinary companions, and a companion is
	// worth what its atomicity with the row is worth. Written after this
	// method's own transaction has committed, they are a window in which the
	// definition exists and nothing downstream has been told — narrow,
	// one-directional, and still not something a consumer can close from
	// outside this package.
	//
	// Every check CreateDefinition makes is made here, and every statement runs
	// on q: the name collision check, the definition, its enumeration, and the
	// read-back of the creation time. A value set against the definition later
	// in the same transaction, through SetValueTx, finds it.
	CreateDefinitionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, definition *Definition) (*Definition, error)

	// GetDefinition reads one live definition by id.
	GetDefinition(ctx context.Context, scope tenancy.Scope, definitionID string) (*Definition, error)

	// GetDefinitionByName reads one live definition by the name application
	// code spells. It is the read every value-side call begins with.
	GetDefinitionByName(ctx context.Context, scope tenancy.Scope, name string) (*Definition, error)

	// ListDefinitions pages the scope's catalog.
	ListDefinitions(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Definition], error)

	// UpdateDefinition rewrites a definition, enumeration included.
	//
	// It refuses an edit that some stored value no longer satisfies —
	// ErrStrandedValues, naming the subject and the value — which is the rule
	// this store exists to own. An administrator narrowing an enumeration or
	// changing a kind clears or migrates the offending values first, and the
	// refusal names them one at a time so that there is always something to do
	// next.
	UpdateDefinition(ctx context.Context, scope tenancy.Scope, definition *Definition) error

	// UpdateDefinitionTx is UpdateDefinition inside the caller's transaction, so
	// the edit commits with whatever the caller records about it. A nil q is an
	// error wrapping ErrNilExecutor. See CreateDefinitionTx for the argument in
	// full.
	//
	// The stranded-value walk runs on q, and that is a difference from the
	// other path worth deciding rather than discovering. UpdateDefinition opens
	// its own transaction, so the walk sees the committed values and nothing
	// else. Here it sees the caller's uncommitted writes too: a value cleared
	// through ClearValueTx earlier in the same transaction is no longer live,
	// and does not strand the edit. That is what lets an administrator clear
	// the offending values and narrow the enumeration in one transaction, with
	// neither landing without the other — and it is the same rule, applied to
	// the values as the transaction sees them rather than as the last commit
	// left them.
	UpdateDefinitionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, definition *Definition) error

	// ArchiveDefinition retires a setting.
	//
	// The values stored against it are left alone and the name stays claimed:
	// archiving is not erasure, and freeing the name would let a second
	// definition inherit rows written for the first. A catalog that genuinely
	// wants the name back deletes the definition, which takes its values with
	// it through the schema's cascade.
	ArchiveDefinition(ctx context.Context, scope tenancy.Scope, definitionID string) error

	// ArchiveDefinitionTx is ArchiveDefinition inside the caller's transaction,
	// so the retirement commits with whatever the caller records about it. A
	// nil q is an error wrapping ErrNilExecutor. See CreateDefinitionTx for the
	// argument in full.
	ArchiveDefinitionTx(ctx context.Context, q database.Tx, scope tenancy.Scope, definitionID string) error
}

// ValueStore is the request path: what one subject answered, and what a setting
// resolves to for them.
//
// Every method takes the setting's name rather than a definition id, because the
// name is what application code holds — and the definition read that name costs
// is the read that validates the write anyway, so nothing is saved by making a
// caller do it first.
type ValueStore interface {
	// SetValue stores a subject's answer, replacing whatever they answered
	// before and reviving an answer they had cleared.
	//
	// raw is checked against the definition: of its kind, and in its
	// enumeration where there is one. A value the setting does not admit is
	// ErrMalformedValue or ErrNotEnumerated rather than a row nothing can read
	// back.
	SetValue(ctx context.Context, scope tenancy.Scope, subject Subject, name, raw string) (*Value, error)

	// SetValueTx is SetValue inside the caller's transaction, so the answer
	// commits with whatever the caller writes beside it. A nil q is an error
	// wrapping ErrNilExecutor.
	//
	// It is the variant the request path reaches for when a preference change
	// is also an audit entry and a data change event: the three land together
	// or not at all, where SetValue followed by the companions in a transaction
	// of their own leaves a window in which the value exists and nothing
	// downstream has been told. See CreateDefinitionTx for the argument in
	// full.
	//
	// The definition read that validates the write runs on q, so a definition
	// created through CreateDefinitionTx earlier in the same transaction is one
	// a value can be set against.
	SetValueTx(ctx context.Context, q database.Tx, scope tenancy.Scope, subject Subject, name, raw string) (*Value, error)

	// GetValue reads the answer a subject stored, or ErrValueNotFound when they
	// have not answered. It is the raw row; Resolve is what applies the
	// default.
	GetValue(ctx context.Context, scope tenancy.Scope, subject Subject, name string) (*Value, error)

	// ClearValue takes a subject's answer back, leaving them on the
	// definition's default.
	ClearValue(ctx context.Context, scope tenancy.Scope, subject Subject, name string) error

	// ClearValueTx is ClearValue inside the caller's transaction, so the
	// clearance commits with whatever the caller records about it. A nil q is
	// an error wrapping ErrNilExecutor. See CreateDefinitionTx for the argument
	// in full, and UpdateDefinitionTx for what clearing a value in the same
	// transaction as an edit to its definition buys.
	ClearValueTx(ctx context.Context, q database.Tx, scope tenancy.Scope, subject Subject, name string) error

	// DeleteValuesForSubject destroys everything one subject answered within
	// the scope — cleared answers included — and reports how many rows that
	// was.
	//
	// It is what an erasure is built on, and it is the one delete here because
	// clearing is not one: ClearValue archives the row, and the row still says
	// what the subject chose. A consumer whose subject access request has to
	// remove a person's stored preferences calls this from the transaction that
	// removes the person, which is why it takes an executor rather than
	// reaching for the store's own. A schema whose values all belong to one
	// subject type can cascade from that table's delete instead, and one with
	// two cannot, because a mixed subject_id column cannot reference two
	// tables.
	//
	// Zero is not an error: a subject who never answered is a subject with
	// nothing here to erase.
	DeleteValuesForSubject(ctx context.Context, q database.Tx, scope tenancy.Scope, subject Subject) (int64, error)

	// ListValuesForSubject pages everything one subject has answered.
	ListValuesForSubject(ctx context.Context, scope tenancy.Scope, subject Subject, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Value], error)

	// ListValuesForDefinition pages everyone who has answered one setting. It is
	// the administrative read behind "who has overridden this", and the walk
	// UpdateDefinition runs before it changes a kind or an enumeration.
	ListValuesForDefinition(ctx context.Context, scope tenancy.Scope, name string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Value], error)

	// Resolve answers one setting for one subject: their value, else the
	// definition's default, else neither.
	//
	// The third case is a resolution rather than an error — the setting exists
	// and has not been answered — and reading it as a typed value reports
	// ErrSettingUnset. A setting that does not exist at all is
	// ErrDefinitionNotFound.
	Resolve(ctx context.Context, scope tenancy.Scope, subject Subject, name string) (*Resolution, error)

	// ResolveAll answers every live setting in the scope for one subject, sorted
	// by name.
	//
	// It is the read a settings page makes, and it is one pass over the
	// catalog and one over the subject's answers rather than one resolution
	// per setting. Settings the subject has not answered are in the result, at
	// their default or as [SourceUnset]: a page rendering "your preferences"
	// wants the ones nobody has touched too.
	ResolveAll(ctx context.Context, scope tenancy.Scope, subject Subject) ([]*Resolution, error)
}
