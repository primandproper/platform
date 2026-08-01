package llm

import (
	"encoding/json"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestCompletionResponse_Text(T *testing.T) {
	T.Parallel()

	T.Run("concatenates text parts and ignores the rest", func(t *testing.T) {
		t.Parallel()

		resp := &CompletionResponse{
			Content: []Part{
				{Type: PartThinking, Text: "reasoning that is not the answer"},
				{Type: PartText, Text: "the "},
				{Type: PartToolUse, ToolUse: &ToolUse{ID: "t"}},
				{Type: PartText, Text: "answer"},
			},
		}

		test.EqOp(t, "the answer", resp.Text())
	})

	T.Run("with a nil receiver", func(t *testing.T) {
		t.Parallel()

		var resp *CompletionResponse

		test.EqOp(t, "", resp.Text())
	})

	T.Run("with no content", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", (&CompletionResponse{}).Text())
	})
}

func TestCompletionResponse_ToolUses(T *testing.T) {
	T.Parallel()

	T.Run("returns tool uses in order", func(t *testing.T) {
		t.Parallel()

		resp := &CompletionResponse{
			Content: []Part{
				{Type: PartText, Text: "calling tools"},
				{Type: PartToolUse, ToolUse: &ToolUse{ID: "a", Name: "first", Input: json.RawMessage(`{"x":1}`)}},
				{Type: PartToolUse, ToolUse: &ToolUse{ID: "b", Name: "second"}},
			},
		}

		uses := resp.ToolUses()
		must.SliceLen(t, 2, uses)
		test.EqOp(t, "a", uses[0].ID)
		test.EqOp(t, "first", uses[0].Name)
		test.Eq(t, json.RawMessage(`{"x":1}`), uses[0].Input)
		test.EqOp(t, "b", uses[1].ID)
	})

	T.Run("ignores a tool_use part with no tool use", func(t *testing.T) {
		t.Parallel()

		resp := &CompletionResponse{Content: []Part{{Type: PartToolUse}}}

		test.SliceEmpty(t, resp.ToolUses())
	})

	T.Run("with a nil receiver", func(t *testing.T) {
		t.Parallel()

		var resp *CompletionResponse

		test.SliceEmpty(t, resp.ToolUses())
	})

	T.Run("with no tool uses", func(t *testing.T) {
		t.Parallel()

		resp := &CompletionResponse{Content: []Part{{Type: PartText, Text: "done"}}}

		test.SliceEmpty(t, resp.ToolUses())
	})
}
