package migrate

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestAnnotateSQL(T *testing.T) {
	T.Parallel()

	T.Run("inserts the Up annotation into a bare migration", func(t *testing.T) {
		t.Parallel()

		got, err := annotateSQL("00001_x.sql", []byte("CREATE TABLE t (id INT);\n"))
		must.NoError(t, err)
		test.EqOp(t, "-- +goose Up\nCREATE TABLE t (id INT);\n", string(got))
	})

	T.Run("leaves an already-annotated migration byte-identical", func(t *testing.T) {
		t.Parallel()

		original := "-- +goose Up\nCREATE TABLE t (id INT);\n\n-- +goose Down\nDROP TABLE t;\n"

		got, err := annotateSQL("00001_x.sql", []byte(original))
		must.NoError(t, err)
		test.EqOp(t, original, string(got))
	})

	T.Run("leaves a Down-first migration for goose to reject", func(t *testing.T) {
		t.Parallel()

		// Malformed, but goose's own error names the problem precisely.
		// Injecting an Up above it would only muddy that.
		original := "-- +goose Down\nDROP TABLE t;\n"

		got, err := annotateSQL("00001_x.sql", []byte(original))
		must.NoError(t, err)
		test.EqOp(t, original, string(got))
	})

	T.Run("does not inject into a miscased annotation", func(t *testing.T) {
		t.Parallel()

		// goose rejects "up" as an unknown annotation. The author clearly
		// meant to annotate, so let that error surface rather than adding a
		// second, duplicate-looking one.
		original := "-- +goose up\nCREATE TABLE t (id INT);\n"

		got, err := annotateSQL("00001_x.sql", []byte(original))
		must.NoError(t, err)
		test.EqOp(t, original, string(got))
	})

	T.Run("inserts below a preamble annotation", func(t *testing.T) {
		t.Parallel()

		for _, preamble := range []string{"-- +goose NO TRANSACTION", "-- +goose ENVSUB ON"} {
			t.Run(preamble, func(t *testing.T) {
				t.Parallel()

				got, err := annotateSQL("00001_x.sql", []byte(preamble+"\nCREATE INDEX i ON t (c);\n"))
				must.NoError(t, err)

				lines := strings.Split(string(got), "\n")
				// The preamble has to stay first: it configures how the
				// section that follows is run.
				test.EqOp(t, preamble, lines[0])
				test.EqOp(t, gooseUpAnnotation, lines[1])
			})
		}
	})

	T.Run("inserts below leading blank lines", func(t *testing.T) {
		t.Parallel()

		got, err := annotateSQL("00001_x.sql", []byte("\n\nCREATE TABLE t (id INT);\n"))
		must.NoError(t, err)

		lines := strings.Split(string(got), "\n")
		test.EqOp(t, gooseUpAnnotation, lines[2])
	})

	T.Run("inserts above a leading plain comment", func(t *testing.T) {
		t.Parallel()

		got, err := annotateSQL("00001_x.sql", []byte("-- describes the table\nCREATE TABLE t (id INT);\n"))
		must.NoError(t, err)

		lines := strings.Split(string(got), "\n")
		test.EqOp(t, gooseUpAnnotation, lines[0])
		test.EqOp(t, "-- describes the table", lines[1])
	})

	T.Run("rejects an unfenced dollar-quoted body", func(t *testing.T) {
		t.Parallel()

		// Without a fence goose would split this on the semicolon inside the
		// body. Before injection the file failed loudly for want of an Up;
		// this keeps it failing loudly rather than mis-executing.
		for _, body := range []string{
			"CREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n  PERFORM 1;\nEND;\n$$ LANGUAGE plpgsql;\n",
			"CREATE FUNCTION f() RETURNS void AS $body$\nBEGIN\n  PERFORM 1;\nEND;\n$body$ LANGUAGE plpgsql;\n",
		} {
			_, err := annotateSQL("00007_fn.sql", []byte(body))
			must.Error(t, err)
			test.StrContains(t, err.Error(), "00007_fn.sql")
			test.StrContains(t, err.Error(), "StatementBegin")
		}
	})

	T.Run("accepts a fenced dollar-quoted body", func(t *testing.T) {
		t.Parallel()

		body := "-- +goose StatementBegin\nCREATE FUNCTION f() RETURNS void AS $$\nBEGIN\n  PERFORM 1;\nEND;\n$$ LANGUAGE plpgsql;\n-- +goose StatementEnd\n"

		got, err := annotateSQL("00007_fn.sql", []byte(body))
		must.NoError(t, err)

		// The fence must end up inside the section, not above it: goose
		// rejects a StatementBegin that precedes the direction annotation.
		lines := strings.Split(string(got), "\n")
		test.EqOp(t, gooseUpAnnotation, lines[0])
		test.EqOp(t, "-- +goose StatementBegin", lines[1])
	})

	T.Run("does not flag dollar quoting in an already-annotated file", func(t *testing.T) {
		t.Parallel()

		// The guard exists to protect files we modify. An author who wrote the
		// Up annotation owns their own fencing, and goose reports it.
		original := "-- +goose Up\nCREATE FUNCTION f() RETURNS void AS $$ BEGIN; END; $$ LANGUAGE plpgsql;\n"

		got, err := annotateSQL("00007_fn.sql", []byte(original))
		must.NoError(t, err)
		test.EqOp(t, original, string(got))
	})
}

func TestAnnotateMigrations(T *testing.T) {
	T.Parallel()

	T.Run("annotates sql files and passes others through", func(t *testing.T) {
		t.Parallel()

		in := fstest.MapFS{
			"00001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id INT);\n")},
			"00002_b.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nCREATE TABLE b (id INT);\n")},
			"helpers.go":  &fstest.MapFile{Data: []byte("package migrations\n")},
		}

		out, err := annotateMigrations(in)
		must.NoError(t, err)

		bare, err := fs.ReadFile(out, "00001_a.sql")
		must.NoError(t, err)
		test.StrHasPrefix(t, gooseUpAnnotation, string(bare))

		// Copied through untouched, so a Go migration source is not mangled.
		helpers, err := fs.ReadFile(out, "helpers.go")
		must.NoError(t, err)
		test.EqOp(t, "package migrations\n", string(helpers))
	})

	T.Run("the result globs the way goose expects", func(t *testing.T) {
		t.Parallel()

		// goose finds migrations with fs.Glob(fsys, "*.sql") at the top level,
		// so the wrapper has to support that, not just Open.
		in := fstest.MapFS{
			"00001_a.sql": &fstest.MapFile{Data: []byte("CREATE TABLE a (id INT);\n")},
			"00002_b.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id INT);\n")},
		}

		out, err := annotateMigrations(in)
		must.NoError(t, err)

		matches, err := fs.Glob(out, "*.sql")
		must.NoError(t, err)
		test.SliceLen(t, 2, matches)
	})

	T.Run("skips directories", func(t *testing.T) {
		t.Parallel()

		in := fstest.MapFS{
			"00001_a.sql":      &fstest.MapFile{Data: []byte("CREATE TABLE a (id INT);\n")},
			"nested/00002.sql": &fstest.MapFile{Data: []byte("CREATE TABLE b (id INT);\n")},
		}

		out, err := annotateMigrations(in)
		must.NoError(t, err)

		matches, err := fs.Glob(out, "*.sql")
		must.NoError(t, err)
		test.SliceLen(t, 1, matches)
	})

	T.Run("surfaces a bad migration as a construction error", func(t *testing.T) {
		t.Parallel()

		in := fstest.MapFS{
			"00001_fn.sql": &fstest.MapFile{
				Data: []byte("CREATE FUNCTION f() RETURNS void AS $$ BEGIN; END; $$ LANGUAGE plpgsql;\n"),
			},
		}

		_, err := annotateMigrations(in)
		test.Error(t, err)
	})
}
