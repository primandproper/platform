package catalogen

import (
	"bytes"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

const (
	// defaultSuffix is the constant-name suffix collected when Options.Suffix is
	// empty. See the package comment for why it is the short form.
	defaultSuffix = "EventType"

	// defaultVarName is the name of the generated variable when
	// Options.VarName is empty.
	defaultVarName = "Catalog"

	// outputFileMode is the mode the generated file is created with, and
	// outputDirMode the mode of any parent directory. Both are restrictive
	// because that is the safe thing to create a file as; git records only the
	// executable bit, so the checked-in file is unaffected either way. Note
	// that os.WriteFile does not chmod a file that already exists, so this
	// applies on first generation only.
	outputFileMode fs.FileMode = 0o600
	outputDirMode  fs.FileMode = 0o750
)

var (
	// ErrNoDir indicates Options.Dir was empty. It wraps
	// errors.ErrEmptyInputParameter, so a caller may check either.
	ErrNoDir = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "no directory to scan for event type constants")

	// ErrNoOutputPath indicates Options.OutputPath was empty. It wraps
	// errors.ErrEmptyInputParameter, so a caller may check either.
	ErrNoOutputPath = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "no output path for the generated catalog")

	// ErrInvalidPackageName indicates Options.Package is not a Go identifier,
	// which includes the case where it was derived from an output directory
	// whose name is not one.
	ErrInvalidPackageName = platformerrors.New("generated package name is not a Go identifier")

	// ErrInvalidVarName indicates Options.VarName is not a Go identifier.
	ErrInvalidVarName = platformerrors.New("generated variable name is not a Go identifier")

	// ErrNoEventTypes indicates the scan found nothing. It is an error rather
	// than an empty catalog because an empty catalog rejects every dispatch,
	// and the cause is a mistyped Dir or Suffix far more often than an
	// application that publishes no events.
	ErrNoEventTypes = platformerrors.New("no event type constants found")

	// ErrDuplicateEventType indicates two constants declare the same event
	// type. The catalog is keyed by event type, so one of them would silently
	// win; which one is not something this package should decide.
	ErrDuplicateEventType = platformerrors.New("duplicate event type")

	// ErrNotAStringConstant indicates a constant matched the suffix but its
	// value is not a string literal — an iota enum, or an alias for another
	// constant. It is reported rather than skipped, because skipping it is what
	// produces the missing catalog entry this package exists to prevent.
	ErrNotAStringConstant = platformerrors.New("event type constant is not a string literal")

	// ErrEmptyEventType indicates a constant matched the suffix and holds the
	// empty string, which is not an event type anything can subscribe to.
	ErrEmptyEventType = platformerrors.New("event type constant is empty")

	// ErrCatalogOutOfDate indicates the committed catalog does not match what
	// the constants generate. It is what Check returns, and the error it wraps
	// names the events that differ.
	ErrCatalogOutOfDate = platformerrors.New("committed catalog does not match the event type constants")
)

// Options describes one catalog: which constants it is derived from, and where
// the generated file goes.
type Options struct {
	// Dir is the single tree searched, relative to the working directory or
	// absolute. It must not be empty. Files ending in _test.go, directories
	// named vendor or testdata, and directories whose names begin with "." or
	// "_" are skipped, as is OutputPath itself.
	//
	// It is one tree rather than several deliberately; see the package comment.
	Dir string

	// Suffix is the constant-name suffix that marks an event type. It defaults
	// to "EventType", which also matches the longer forms most constants use
	// (FooServiceEventType), and that is the point — see the package comment.
	Suffix string

	// OutputPath is the file written by Generate and read by Check. Parent
	// directories are created as needed. It must not be empty.
	OutputPath string

	// Package is the package clause of the generated file. It defaults to the
	// name of OutputPath's directory, which is the conventional answer; set it
	// when the directory's name is not a Go identifier or not the package name.
	Package string

	// VarName is the name of the generated webhooks.Catalog variable. It
	// defaults to "Catalog", so the generated declaration reads
	// catalog.Catalog from a package named for what it holds.
	VarName string
}

// Generate derives the catalog from the constants under Dir and writes it to
// OutputPath, creating parent directories as needed.
//
// The output is deterministic: entries are sorted by event type, so two runs
// over an unchanged tree produce byte-identical files and a regeneration shows
// up in a diff only where the constants actually moved.
//
// Nothing is written if the derivation fails, so a tree with a duplicate or
// non-string event type constant leaves the previous catalog in place rather
// than replacing it with a partial one.
//
//nolint:gocritic // hugeParam: Options is the caller-facing struct literal; taking it by pointer would make every call site declare a variable first.
func Generate(opts Options) error {
	src, err := build(&opts)
	if err != nil {
		return err
	}

	if dir := filepath.Dir(opts.OutputPath); dir != "" {
		if err = os.MkdirAll(dir, outputDirMode); err != nil {
			return platformerrors.Wrapf(err, "creating output directory %s", dir)
		}
	}

	if err = os.WriteFile(opts.OutputPath, src, outputFileMode); err != nil {
		return platformerrors.Wrapf(err, "writing catalog to %s", opts.OutputPath)
	}

	return nil
}

// Check derives the catalog the same way Generate does and compares it to the
// file already at OutputPath, writing nothing. It returns nil when they match
// and an error wrapping ErrCatalogOutOfDate when they do not, naming the event
// types that were added, removed, or redescribed.
//
// This is the CI assertion. Regenerating in CI proves only that the generator
// runs; comparing proves that the committed catalog is the one the constants
// describe, which is the claim a reviewer cannot check by eye.
//
// It compares the whole file rather than the entries alone, so it also catches
// a generated file edited by hand and one left behind by an older version of
// this generator. That means it must be given the same Options as Generate, and
// run from the same working directory, which a Makefile target for each keeps
// true by construction.
//
//nolint:gocritic // hugeParam: Options is the caller-facing struct literal; taking it by pointer would make every call site declare a variable first.
func Check(opts Options) error {
	want, err := build(&opts)
	if err != nil {
		return err
	}

	committed, err := os.ReadFile(opts.OutputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return platformerrors.Wrapf(ErrCatalogOutOfDate, "no catalog at %s", opts.OutputPath)
		}

		return platformerrors.Wrapf(err, "reading catalog at %s", opts.OutputPath)
	}

	if bytes.Equal(committed, want) {
		return nil
	}

	resolved, err := opts.normalized()
	if err != nil {
		return err
	}

	return platformerrors.Wrapf(ErrCatalogOutOfDate, "%s: %s", opts.OutputPath, describeDrift(committed, want, resolved.VarName))
}

// build resolves the options, collects the constants, and renders the file, so
// that Generate and Check cannot disagree about what the catalog is.
func build(opts *Options) ([]byte, error) {
	resolved, err := opts.normalized()
	if err != nil {
		return nil, err
	}

	entries, err := collect(resolved)
	if err != nil {
		return nil, err
	}

	return render(resolved, entries)
}

// normalized returns a copy of the options with the documented defaults applied
// and then validated, in that order: an unset field with a default is not a
// caller error, and validating first would make the common case one.
func (o *Options) normalized() (*Options, error) {
	if o.Dir == "" {
		return nil, ErrNoDir
	}

	if o.OutputPath == "" {
		return nil, ErrNoOutputPath
	}

	resolved := *o

	if resolved.Suffix == "" {
		resolved.Suffix = defaultSuffix
	}

	if resolved.VarName == "" {
		resolved.VarName = defaultVarName
	}

	if resolved.Package == "" {
		resolved.Package = filepath.Base(filepath.Dir(resolved.OutputPath))
	}

	if !isIdentifier(resolved.Package) {
		return nil, platformerrors.Wrapf(ErrInvalidPackageName, "%q", resolved.Package)
	}

	if !isIdentifier(resolved.VarName) {
		return nil, platformerrors.Wrapf(ErrInvalidVarName, "%q", resolved.VarName)
	}

	return &resolved, nil
}

// isIdentifier reports whether name can appear as a package name or a
// declaration name in the generated file. A keyword passes token.IsIdentifier
// and would produce a file that does not compile, which is a worse error to
// receive than this one.
func isIdentifier(name string) bool {
	return token.IsIdentifier(name) && !token.IsKeyword(name)
}
