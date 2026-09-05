package sentinelmatrix_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/primandproper/platform-go/v14/dataprivacy"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	grpcerrors "github.com/primandproper/platform-go/v14/errors/grpc"
	httperrors "github.com/primandproper/platform-go/v14/errors/http"
	"github.com/primandproper/platform-go/v14/links"
	"github.com/primandproper/platform-go/v14/operations"
	"github.com/primandproper/platform-go/v14/sessions"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"google.golang.org/grpc/codes"
)

// disposition is what this module has decided one sentinel means on the wire.
// The three are exhaustive by construction: a sentinel either has a case in its
// own package's mappers, or wraps something the platform mappers answer, or
// resolves to a 500.
type disposition int

const (
	// mapped: the package's own HTTPMapper and GRPCMapper both answer.
	mapped disposition = iota
	// platform: those two are silent and errors/http and errors/grpc answer,
	// because the sentinel wraps a platform one.
	platform
	// unhandled: nobody answers, and a 500 is the honest reply.
	unhandled
)

func (d disposition) String() string {
	switch d {
	case mapped:
		return "mapped by its own package"
	case platform:
		return "mapped by the platform mappers"
	case unhandled:
		return "deliberately unmapped"
	default:
		return "unknown"
	}
}

type decision struct {
	err error
	is  disposition
}

// matrix is the decision made about every exported sentinel in the four
// packages that map their own errors. Its keys are checked against those
// packages' source in both directions, so it is a roster that cannot quietly
// stop describing the tree.
var matrix = map[string]map[string]decision{
	"dataprivacy": {
		// A subject asking after their own export or erasure is a client. These five
		// are the answers they can act on: the ID is not one of theirs, the request
		// is not in the state the call needs, or the request they sent is malformed.
		"ErrArtifactUnavailable":     {err: dataprivacy.ErrArtifactUnavailable, is: mapped},
		"ErrEmptySubjectID":          {err: dataprivacy.ErrEmptySubjectID, is: mapped},
		"ErrNotAwaitingConfirmation": {err: dataprivacy.ErrNotAwaitingConfirmation, is: mapped},
		"ErrRequestNotFound":         {err: dataprivacy.ErrRequestNotFound, is: mapped},
		"ErrUnknownRequestType":      {err: dataprivacy.ErrUnknownRequestType, is: mapped},

		// The nil-argument sentinels, which wrap errors.ErrNilInputParameter and are
		// answered by the platform mapper for that reason. They are wiring failures
		// and the 400 they resolve to is generous; the mapping predates this package
		// and is not this package's to change.
		"ErrNilDatabaseClient": {err: dataprivacy.ErrNilDatabaseClient, is: platform},
		"ErrNilExecutor":       {err: dataprivacy.ErrNilExecutor, is: platform},
		"ErrNilFetch":          {err: dataprivacy.ErrNilFetch, is: platform},
		"ErrNilOperations":     {err: dataprivacy.ErrNilOperations, is: platform},
		"ErrNilRequest":        {err: dataprivacy.ErrNilRequest, is: platform},
		"ErrNilStore":          {err: dataprivacy.ErrNilStore, is: platform},

		// Fulfillment-side outcomes and construction failures. A collector that
		// panicked, an export document too large to store, an upload manager that
		// cannot sign a URL, a registry with a duplicate key: none is a request a
		// subject could have sent differently, and a 500 is the honest answer.
		// ErrNilPage and ErrCursorStalled are a Store misbehaving toward its own
		// caller and never reach a handler at all.
		"ErrArtifactEncrypted":  {err: dataprivacy.ErrArtifactEncrypted, is: unhandled},
		"ErrCollectorPanicked":  {err: dataprivacy.ErrCollectorPanicked, is: unhandled},
		"ErrCursorStalled":      {err: dataprivacy.ErrCursorStalled, is: unhandled},
		"ErrDocumentTooLarge":   {err: dataprivacy.ErrDocumentTooLarge, is: unhandled},
		"ErrDuplicateKey":       {err: dataprivacy.ErrDuplicateKey, is: unhandled},
		"ErrEraserPanicked":     {err: dataprivacy.ErrEraserPanicked, is: unhandled},
		"ErrEverySectionFailed": {err: dataprivacy.ErrEverySectionFailed, is: unhandled},
		"ErrInvalidFragment":    {err: dataprivacy.ErrInvalidFragment, is: unhandled},
		"ErrInvalidKey":         {err: dataprivacy.ErrInvalidKey, is: unhandled},
		"ErrNilPage":            {err: dataprivacy.ErrNilPage, is: unhandled},
		"ErrNoCollectors":       {err: dataprivacy.ErrNoCollectors, is: unhandled},
		"ErrNoErasers":          {err: dataprivacy.ErrNoErasers, is: unhandled},
		"ErrNoURLSigner":        {err: dataprivacy.ErrNoURLSigner, is: unhandled},
		"ErrNoUploadManager":    {err: dataprivacy.ErrNoUploadManager, is: unhandled},
		"ErrNotInProgress":      {err: dataprivacy.ErrNotInProgress, is: unhandled},
		"ErrUnexpiringArtifact": {err: dataprivacy.ErrUnexpiringArtifact, is: unhandled},
		"ErrUnknownStatus":      {err: dataprivacy.ErrUnknownStatus, is: unhandled},
	},
	"links": {
		// The four redemption outcomes and the malformed token. These are the whole
		// of what a person holding a link can be told.
		"ErrInvalidToken":        {err: links.ErrInvalidToken, is: mapped},
		"ErrLinkAlreadyRedeemed": {err: links.ErrLinkAlreadyRedeemed, is: mapped},
		"ErrLinkExpired":         {err: links.ErrLinkExpired, is: mapped},
		"ErrLinkNotFound":        {err: links.ErrLinkNotFound, is: mapped},
		"ErrLinkRevoked":         {err: links.ErrLinkRevoked, is: mapped},

		// Wraps errors.ErrNilInputParameter, and the platform mapper answers it.
		"ErrNilStore": {err: links.ErrNilStore, is: platform},

		// Minter construction — an unregistered action, an unusable URL template, a
		// non-positive TTL — and the store reporting itself. ErrStoreUnavailable is
		// the one worth pausing on: redemption fails closed on it, and it is a 500
		// deliberately, because a link this package cannot prove is unused is not a
		// link the bearer should be told anything specific about.
		"ErrEmptySubject":      {err: links.ErrEmptySubject, is: unhandled},
		"ErrInsecureActionURL": {err: links.ErrInsecureActionURL, is: unhandled},
		"ErrInvalidActionURL":  {err: links.ErrInvalidActionURL, is: unhandled},
		"ErrInvalidID":         {err: links.ErrInvalidID, is: unhandled},
		"ErrInvalidTTL":        {err: links.ErrInvalidTTL, is: unhandled},
		"ErrNoActions":         {err: links.ErrNoActions, is: unhandled},
		"ErrStaleRecord":       {err: links.ErrStaleRecord, is: unhandled},
		"ErrStoreUnavailable":  {err: links.ErrStoreUnavailable, is: unhandled},
		"ErrUnknownAction":     {err: links.ErrUnknownAction, is: unhandled},
	},
	"operations": {
		// A missing operation, which is also what an operation belonging to somebody
		// else reads as, and a subscription refused for capacity.
		"ErrOperationNotFound": {err: operations.ErrOperationNotFound, is: mapped},
		"ErrTooManyWatchers":   {err: operations.ErrTooManyWatchers, is: mapped},

		// The nil-argument sentinels, which wrap errors.ErrNilInputParameter.
		"ErrNilConfig":         {err: operations.ErrNilConfig, is: platform},
		"ErrNilDatabaseClient": {err: operations.ErrNilDatabaseClient, is: platform},
		"ErrNilExecutor":       {err: operations.ErrNilExecutor, is: platform},
		"ErrNilOperation":      {err: operations.ErrNilOperation, is: platform},
		"ErrNilQueue":          {err: operations.ErrNilQueue, is: platform},
		"ErrNilRegistry":       {err: operations.ErrNilRegistry, is: platform},
		"ErrNilService":        {err: operations.ErrNilService, is: platform},
		"ErrNilStore":          {err: operations.ErrNilStore, is: platform},

		// Registry and worker outcomes: a kind registered twice, a runner that
		// panicked, a result too large to record, a watcher used after close. They
		// describe the service rather than the request, and a 500 is the honest
		// answer. ErrRequestTooLarge is the near miss — it is about something a
		// caller sent — but it is raised by the service enqueuing work rather than
		// by a handler decoding a request, and nothing today puts it on a response.
		"ErrDuplicateKind":       {err: operations.ErrDuplicateKind, is: unhandled},
		"ErrDuplicateOperation":  {err: operations.ErrDuplicateOperation, is: unhandled},
		"ErrInvalidDefinition":   {err: operations.ErrInvalidDefinition, is: unhandled},
		"ErrRequestTooLarge":     {err: operations.ErrRequestTooLarge, is: unhandled},
		"ErrRequestTypeMismatch": {err: operations.ErrRequestTypeMismatch, is: unhandled},
		"ErrResultTooLarge":      {err: operations.ErrResultTooLarge, is: unhandled},
		"ErrRunnerPanicked":      {err: operations.ErrRunnerPanicked, is: unhandled},
		"ErrUnknownKind":         {err: operations.ErrUnknownKind, is: unhandled},
		"ErrWatcherClosed":       {err: operations.ErrWatcherClosed, is: unhandled},
	},
	"sessions": {
		// Every unusable session. The two timeouts wrap ErrExpired, which wraps
		// ErrNotFound, so all four resolve; they are listed because a sentinel that
		// stops wrapping is exactly the kind of change this roster is here to catch.
		"ErrAbsoluteTimeout": {err: sessions.ErrAbsoluteTimeout, is: mapped},
		"ErrExpired":         {err: sessions.ErrExpired, is: mapped},
		"ErrIdleTimeout":     {err: sessions.ErrIdleTimeout, is: mapped},
		"ErrNotFound":        {err: sessions.ErrNotFound, is: mapped},

		// Wrap errors.ErrEmptyInputParameter and errors.ErrNilInputParameter.
		"ErrIDRequired":        {err: sessions.ErrIDRequired, is: platform},
		"ErrNilBackend":        {err: sessions.ErrNilBackend, is: platform},
		"ErrPrincipalRequired": {err: sessions.ErrPrincipalRequired, is: platform},

		// A backend handed an identifier it did not mint, a backend that keeps no
		// principal index, and the three Policy validation failures. None is
		// something a client sent.
		"ErrIDConflict":              {err: sessions.ErrIDConflict, is: unhandled},
		"ErrNegativeTouchInterval":   {err: sessions.ErrNegativeTouchInterval, is: unhandled},
		"ErrNoPrincipalIndex":        {err: sessions.ErrNoPrincipalIndex, is: unhandled},
		"ErrNoTimeout":               {err: sessions.ErrNoTimeout, is: unhandled},
		"ErrTouchExceedsIdleTimeout": {err: sessions.ErrTouchExceedsIdleTimeout, is: unhandled},
	},
}

// packages are the directories matrix's rows are read out of, relative to the
// module root. They are the four that export mappers of their own; a fifth
// would be added here and in matrix together.
var packages = []string{"dataprivacy", "links", "operations", "sessions"}

// TestEverySentinelHasADecision is the entry this package exists to make
// impossible to forget. A sentinel added to one of these packages and named in
// no row has had no decision made about it, which is the state where it reaches
// a client as a 500 on one transport and codes.Unknown on the other while every
// test in its own package stays green.
func TestEverySentinelHasADecision(T *testing.T) {
	T.Parallel()

	for _, pkg := range packages {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			declared := sentinelNames(t, pkg)
			must.SliceNotEmpty(t, declared, must.Sprintf("no sentinels parsed out of %s", pkg))

			for _, name := range declared {
				_, ok := matrix[pkg][name]
				test.True(t, ok, test.Sprintf(
					"%s.%s is a sentinel with no row here, so nothing says what a client is told when it happens", pkg, name))
			}
		})
	}
}

// TestNoRowOutlivesItsSentinel is the other direction, and the one a rename or a
// deletion breaks. A row naming a sentinel that is no longer there reads exactly
// like a live one, and a reader counting the mapped rows would be counting a
// mapping nothing produces.
func TestNoRowOutlivesItsSentinel(T *testing.T) {
	T.Parallel()

	for _, pkg := range packages {
		T.Run(pkg, func(t *testing.T) {
			t.Parallel()

			declared := sentinelNames(t, pkg)

			for name := range matrix[pkg] {
				test.True(t, slices.Contains(declared, name), test.Sprintf(
					"%s.%s has a row here and is not a sentinel in that package any more", pkg, name))
			}
		})
	}
}

// TestEveryDecisionHoldsOnBothTransports checks the rows against what the
// mappers actually do. A row is a claim about a client's experience, and a claim
// nothing verifies is how the gRPC mapper came to be missing sessions and
// operations in the first place: an expired session reached an HTTP client as a
// considered 401 and a gRPC client as codes.Unknown, and which one you got
// depended on how you had connected.
func TestEveryDecisionHoldsOnBothTransports(T *testing.T) {
	T.Parallel()

	for _, pkg := range packages {
		for name, row := range matrix[pkg] {
			T.Run(pkg+"."+name, func(t *testing.T) {
				t.Parallel()

				// Bare and wrapped, because a handler wraps: a mapping that only
				// works on the sentinel itself works nowhere real.
				assertDecision(t, pkg, name, row, row.err)
				assertDecision(t, pkg, name, row, platformerrors.Wrap(row.err, "doing the thing"))
			})
		}
	}
}

// assertDecision checks one row against both transports, through the package's
// own mappers and through the platform ones.
//
// It asks the mappers directly rather than through ToAPIError and MapToGRPC,
// which would answer out of a process-global registry: whether somebody has
// called RegisterHTTPErrorMapper is a property of a binary's wiring, and this
// package is about whether the mapping exists to be registered.
func assertDecision(t *testing.T, pkg, name string, row decision, err error) {
	t.Helper()

	httpDomain, grpcDomain := domainMappers(pkg)

	_, _, byDomainHTTP := httpDomain.Map(err)
	_, byDomainGRPC := grpcDomain.Map(err)

	_, _, byPlatformHTTP := httperrors.PlatformMapper.Map(err)
	byPlatformCode, byPlatformGRPC := grpcerrors.PlatformMapper.Map(err)

	switch row.is {
	case mapped:
		test.True(t, byDomainHTTP, test.Sprintf("%s.%s is %v and %s.HTTPMapper does not answer it", pkg, name, row.is, pkg))
		test.True(t, byDomainGRPC, test.Sprintf("%s.%s is %v and %s.GRPCMapper does not answer it", pkg, name, row.is, pkg))
	case platform:
		test.False(t, byDomainHTTP, test.Sprintf("%s.%s is %v and %s.HTTPMapper claims it too", pkg, name, row.is, pkg))
		test.False(t, byDomainGRPC, test.Sprintf("%s.%s is %v and %s.GRPCMapper claims it too", pkg, name, row.is, pkg))
		test.True(t, byPlatformHTTP, test.Sprintf("%s.%s is %v and errors/http does not answer it", pkg, name, row.is))
		test.True(t, byPlatformGRPC, test.Sprintf("%s.%s is %v and errors/grpc does not answer it", pkg, name, row.is))
	case unhandled:
		test.False(t, byDomainHTTP, test.Sprintf("%s.%s is %v and %s.HTTPMapper answers it", pkg, name, row.is, pkg))
		test.False(t, byDomainGRPC, test.Sprintf("%s.%s is %v and %s.GRPCMapper answers it", pkg, name, row.is, pkg))
		test.False(t, byPlatformHTTP, test.Sprintf("%s.%s is %v and errors/http answers it", pkg, name, row.is))
		test.False(t, byPlatformGRPC, test.Sprintf("%s.%s is %v and errors/grpc answers it", pkg, name, row.is))
		test.EqOp(t, codes.Unknown, byPlatformCode)
	default:
		t.Fatalf("%s.%s carries no disposition", pkg, name)
	}
}

// domainMappers is the pair of mappers a package exports. The switch is the one
// place this package spells the four out; everywhere else they are the strings
// in packages.
func domainMappers(pkg string) (httperrors.HTTPErrorMapper, grpcerrors.GRPCErrorMapper) {
	switch pkg {
	case "dataprivacy":
		return dataprivacy.HTTPMapper, dataprivacy.GRPCMapper
	case "links":
		return links.HTTPMapper, links.GRPCMapper
	case "operations":
		return operations.HTTPMapper, operations.GRPCMapper
	case "sessions":
		return sessions.HTTPMapper, sessions.GRPCMapper
	default:
		panic("no mappers for " + pkg)
	}
}

// sentinelNames is every exported name beginning with Err declared as a
// package-level var in pkg.
//
// The ground truth is deliberately crude, in the manner of
// internal/transportmatrix: a var whose name starts with Err is a sentinel. That
// finds one by how it is written rather than by anything it declares, which is
// the property that matters — a sentinel added in the ordinary way is precisely
// the one to catch.
var parsed = sync.OnceValues(func() (map[string][]string, error) {
	found := map[string][]string{}

	for _, pkg := range packages {
		dir := filepath.Join(moduleRootPath(), pkg)

		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}

		var names []string

		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}

			file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
			if parseErr != nil {
				return nil, parseErr
			}

			names = append(names, sentinelsIn(file)...)
		}

		slices.Sort(names)
		found[pkg] = names
	}

	return found, nil
})

func sentinelNames(t *testing.T, pkg string) []string {
	t.Helper()

	found, err := parsed()
	must.NoError(t, err)

	return found[pkg]
}

// sentinelsIn reads one file's package-level Err vars.
func sentinelsIn(file *ast.File) []string {
	var names []string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}

		for _, spec := range gen.Specs {
			value, isValue := spec.(*ast.ValueSpec)
			if !isValue {
				continue
			}

			for _, ident := range value.Names {
				if strings.HasPrefix(ident.Name, "Err") && ident.IsExported() {
					names = append(names, ident.Name)
				}
			}
		}
	}

	return names
}

// TestErrorsDoesNotImportTheTierAboveIt is the invariant the mappings were moved
// to establish, and the one thing here the compiler does not already enforce.
//
// A non-test file under errors/ that imported one of these four would be an
// import cycle and would not build, so it needs no test. An external test
// package — errors' own mapper_parity_test.go is one — can import them freely,
// which is how the dependency would come back: a test reaching for a domain
// sentinel to assert something about, and errors/ quietly stops being a package
// that can be lifted out on its own.
func TestErrorsDoesNotImportTheTierAboveIt(T *testing.T) {
	T.Parallel()

	root := filepath.Join(moduleRootPath(), "errors")

	var checked int

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}

		checked++

		rel, relErr := filepath.Rel(moduleRootPath(), path)
		if relErr != nil {
			return relErr
		}

		for _, imported := range file.Imports {
			for _, pkg := range packages {
				test.False(T, strings.HasSuffix(strings.Trim(imported.Path.Value, `"`), "/"+pkg), test.Sprintf(
					"%s imports %s, which imports errors/http and errors/grpc — the mappings were moved out of errors/ so that it depends on nothing above it", rel, pkg))
			}
		}

		return nil
	})
	must.NoError(T, err)

	test.Greater(T, 0, checked, test.Sprint("no files parsed under errors/, so this test asserted nothing"))
}

// TestModuleRootIsThisModule keeps the walk above honest. A test binary run from
// anywhere but this package's directory would read four directories that are not
// these, or none, and a roster that matches nothing would report as a roster that
// matches everything — except that TestEverySentinelHasADecision insists on a
// non-empty parse, which is the other half of the same guard.
func TestModuleRootIsThisModule(T *testing.T) {
	T.Parallel()

	must.FileExists(T, filepath.Join(moduleRootPath(), "go.mod"))

	for _, pkg := range packages {
		must.DirExists(T, filepath.Join(moduleRootPath(), pkg))
	}
}

// moduleRootPath is two directories up, which is where this package sits and
// where go.mod has to be for the answer to be this module rather than whatever
// tree a test binary was copied into.
var moduleRootPath = sync.OnceValue(func() string {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic(err)
	}

	return root
})
