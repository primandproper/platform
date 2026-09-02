package settings

import (
	"context"

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

	// ArchiveDefinition retires a setting.
	//
	// The values stored against it are left alone and the name stays claimed:
	// archiving is not erasure, and freeing the name would let a second
	// definition inherit rows written for the first. A catalog that genuinely
	// wants the name back deletes the definition, which takes its values with
	// it through the schema's cascade.
	ArchiveDefinition(ctx context.Context, scope tenancy.Scope, definitionID string) error
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

	// GetValue reads the answer a subject stored, or ErrValueNotFound when they
	// have not answered. It is the raw row; Resolve is what applies the
	// default.
	GetValue(ctx context.Context, scope tenancy.Scope, subject Subject, name string) (*Value, error)

	// ClearValue takes a subject's answer back, leaving them on the
	// definition's default.
	ClearValue(ctx context.Context, scope tenancy.Scope, subject Subject, name string) error

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
