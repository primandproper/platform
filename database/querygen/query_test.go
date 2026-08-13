package querygen

import (
	"testing"

	"github.com/shoenig/test"
)

func TestQuery_Render(T *testing.T) {
	T.Parallel()

	T.Run("terminates an unterminated statement", func(t *testing.T) {
		t.Parallel()

		q := &Query{
			Annotation: QueryAnnotation{Name: "GetThing", Type: OneType},
			Content:    "SELECT things.id FROM things",
		}

		test.EqOp(t, "-- name: GetThing :one\nSELECT things.id FROM things;\n", q.Render())
	})

	T.Run("does not add a second terminator", func(t *testing.T) {
		t.Parallel()

		q := &Query{
			Annotation: QueryAnnotation{Name: "ArchiveThing", Type: ExecRowsType},
			Content:    "UPDATE things SET archived_at = NOW();",
		}

		test.EqOp(t, "-- name: ArchiveThing :execrows\nUPDATE things SET archived_at = NOW();\n", q.Render())
	})
}

func TestRenderFile(T *testing.T) {
	T.Parallel()

	T.Run("separates statements with a blank line and ends with one newline", func(t *testing.T) {
		t.Parallel()

		got := RenderFile([]*Query{
			{Annotation: QueryAnnotation{Name: "A", Type: ExecType}, Content: "SELECT 1"},
			{Annotation: QueryAnnotation{Name: "B", Type: ExecType}, Content: "SELECT 2"},
		})

		test.EqOp(t, "-- name: A :exec\nSELECT 1;\n\n-- name: B :exec\nSELECT 2;\n", got)
	})

	T.Run("strips trailing whitespace, which is what makes a check run a question about SQL", func(t *testing.T) {
		t.Parallel()

		got := RenderFile([]*Query{
			{Annotation: QueryAnnotation{Name: "A", Type: ExecType}, Content: "SELECT 1 \t\nFROM things;"},
		})

		test.EqOp(t, "-- name: A :exec\nSELECT 1\nFROM things;\n", got)
	})

	T.Run("nothing in, nothing out", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", RenderFile(nil))
	})
}
