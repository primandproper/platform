package migrate

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
	"testing/fstest"

	"github.com/primandproper/platform-go/v7/errors"
)

// generatedMigration is a migration supplied as text rather than as a file on
// disk. Platform packages that own a table — outbox is the first — render their
// DDL from code, and the consumer places it in their own sequence by choosing
// its version.
type generatedMigration struct {
	name    string
	body    string
	version uint64
}

// filename renders the goose filename for this migration. The width matches the
// 00001_description.sql convention and widens naturally for larger versions.
func (g *generatedMigration) filename() string {
	return fmt.Sprintf("%05d_%s.sql", g.version, g.name)
}

// validate rejects a generated migration that could not produce a usable file.
func (g *generatedMigration) validate() error {
	if g.version == 0 {
		return errors.New("generated migration version must be greater than zero")
	}
	if g.name == "" {
		return errors.New("generated migration name is required")
	}
	if !validMigrationName(g.name) {
		return errors.Newf(
			"generated migration name %q must contain only letters, digits and underscores", g.name,
		)
	}
	if strings.TrimSpace(g.body) == "" {
		return errors.Newf("generated migration %q has no SQL", g.name)
	}

	return nil
}

// validMigrationName reports whether name is safe to place in a filename. The
// name lands between the version prefix and the .sql suffix, so anything that
// could introduce a path separator or a second extension is refused.
func validMigrationName(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}

	return true
}

// mergeGenerated adds the generated migrations to an already-annotated
// filesystem, failing on any version that a file on disk already claims.
//
// The collision check is the whole reason this is not just "write another file
// into the map": goose keys applied migrations by version, and two migrations
// sharing one version is a corrupt sequence. Catching it here means a bad
// version fails New — at service construction — rather than mid-deploy on the
// first Migrate.
func mergeGenerated(annotated fs.FS, generated []generatedMigration) (fs.FS, error) {
	if len(generated) == 0 {
		return annotated, nil
	}

	entries, err := fs.ReadDir(annotated, ".")
	if err != nil {
		return nil, errors.Wrap(err, "listing annotated migrations")
	}

	out := make(fstest.MapFS, len(entries)+len(generated))
	claimed := map[uint64]string{}

	for _, entry := range entries {
		name := entry.Name()

		content, readErr := fs.ReadFile(annotated, name)
		if readErr != nil {
			return nil, errors.Wrapf(readErr, "reading migration %q", name)
		}

		out[name] = &fstest.MapFile{Data: content}

		if version, ok := migrationVersion(name); ok {
			claimed[version] = name
		}
	}

	for i := range generated {
		g := &generated[i]

		if err = g.validate(); err != nil {
			return nil, err
		}

		if existing, taken := claimed[g.version]; taken {
			return nil, errors.Newf(
				"generated migration %q wants version %d, which %q already uses; pick an unused version",
				g.name, g.version, existing,
			)
		}

		name := g.filename()

		// Routed through the same annotator as a file on disk, so a generated
		// migration gets the Up annotation and the dollar-quote check on
		// identical terms.
		content, annotateErr := annotateSQL(name, []byte(g.body))
		if annotateErr != nil {
			return nil, annotateErr
		}

		out[name] = &fstest.MapFile{Data: content}
		claimed[g.version] = name
	}

	return out, nil
}

// migrationVersion extracts the leading numeric version from a migration
// filename, the way goose orders them. It reports false for a name with no
// numeric prefix, which goose would ignore anyway.
func migrationVersion(name string) (uint64, bool) {
	if path.Ext(name) != ".sql" {
		return 0, false
	}

	digits := 0
	for digits < len(name) && name[digits] >= '0' && name[digits] <= '9' {
		digits++
	}

	if digits == 0 {
		return 0, false
	}

	var version uint64
	for i := range digits {
		version = version*10 + uint64(name[i]-'0')
	}

	return version, true
}
