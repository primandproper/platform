package llm

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestUserText(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		msg := UserText("hello")

		test.EqOp(t, RoleUser, msg.Role)
		must.SliceLen(t, 1, msg.Content)
		test.EqOp(t, PartText, msg.Content[0].Type)
		test.EqOp(t, "hello", msg.Content[0].Text)
	})

	T.Run("empty text still produces a part", func(t *testing.T) {
		t.Parallel()

		msg := UserText("")

		must.SliceLen(t, 1, msg.Content)
		test.EqOp(t, "", msg.Content[0].Text)
	})
}

func TestAssistantText(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		msg := AssistantText("sure")

		test.EqOp(t, RoleAssistant, msg.Role)
		must.SliceLen(t, 1, msg.Content)
		test.EqOp(t, PartText, msg.Content[0].Type)
		test.EqOp(t, "sure", msg.Content[0].Text)
	})
}

func TestToolResultMessage(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		msg := ToolResultMessage(
			ToolResult{ToolUseID: "a", Content: "1"},
			ToolResult{ToolUseID: "b", Content: "2", IsError: true},
		)

		test.EqOp(t, RoleTool, msg.Role)
		must.SliceLen(t, 2, msg.Content)

		// Each part must point at its own result: a loop that reused one
		// address would leave every part holding the last one.
		must.NotNil(t, msg.Content[0].ToolResult)
		must.NotNil(t, msg.Content[1].ToolResult)
		test.EqOp(t, PartToolResult, msg.Content[0].Type)
		test.EqOp(t, "a", msg.Content[0].ToolResult.ToolUseID)
		test.EqOp(t, "1", msg.Content[0].ToolResult.Content)
		test.False(t, msg.Content[0].ToolResult.IsError)
		test.EqOp(t, "b", msg.Content[1].ToolResult.ToolUseID)
		test.True(t, msg.Content[1].ToolResult.IsError)
	})

	T.Run("with no results", func(t *testing.T) {
		t.Parallel()

		msg := ToolResultMessage()

		test.EqOp(t, RoleTool, msg.Role)
		test.SliceEmpty(t, msg.Content)
	})
}

func TestMessage_Text(T *testing.T) {
	T.Parallel()

	T.Run("concatenates text parts and ignores the rest", func(t *testing.T) {
		t.Parallel()

		msg := Message{
			Role: RoleAssistant,
			Content: []Part{
				{Type: PartThinking, Text: "hmm"},
				{Type: PartText, Text: "one "},
				{Type: PartToolUse, ToolUse: &ToolUse{ID: "t"}},
				{Type: PartText, Text: "two"},
				{Type: PartImage, Image: &Image{URL: "https://example.com/i.png"}},
			},
		}

		test.EqOp(t, "one two", msg.Text())
	})

	T.Run("with no content", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", Message{}.Text())
	})
}
