package hook

import "testing"

// --- Test Cases ---

func TestValidateAdditionalDecisions(t *testing.T) {
	t.Parallel()

	tests := []Response{
		{Decision: DecisionDefer},
		{Decision: DecisionReact, React: &ReactAction{Emoji: "👍"}},
		{Decision: DecisionEdit, Edit: &EditAction{MessageID: "msg-1", Text: "edited"}},
		{Decision: DecisionSetStatus, SetStatus: &SetStatusAction{Presence: "online"}},
	}

	for _, response := range tests {
		response := response
		t.Run(string(response.Decision), func(t *testing.T) {
			t.Parallel()
			if err := response.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestDecodeResponseRejectsEmptyAndMultipleDocuments(t *testing.T) {
	t.Parallel()

	if _, err := DecodeResponse(nil); err == nil {
		t.Fatal("DecodeResponse(nil) error = nil, want empty body error")
	}
	if _, err := DecodeResponse([]byte(`{"decision":"skip"} {"decision":"skip"}`)); err == nil {
		t.Fatal("DecodeResponse(multiple docs) error = nil, want single document error")
	}
}
