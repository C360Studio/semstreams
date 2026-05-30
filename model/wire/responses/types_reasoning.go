package responses

// Variant-type constants. The Responses API discriminates polymorphic
// items via a "type" string field; these constants name the values
// this package recognizes in Phase 1.
const (
	// Input/output item types.
	ItemTypeMessage            = "message"
	ItemTypeFunctionCall       = "function_call"
	ItemTypeFunctionCallOutput = "function_call_output"
	ItemTypeReasoning          = "reasoning"

	// Content-part types.
	ContentTypeInputText  = "input_text"
	ContentTypeOutputText = "output_text"
	ContentTypeRefusal    = "refusal"

	// Summary-part types.
	SummaryTypeText = "summary_text"

	// Role values on message items.
	RoleUser      = "user"
	RoleSystem    = "system"
	RoleDeveloper = "developer"
	RoleAssistant = "assistant"
)

// IsMessage reports whether the item is a message variant.
func (i *InputItem) IsMessage() bool { return i != nil && i.Type == ItemTypeMessage }

// IsFunctionCall reports whether the item is a function-call echo.
func (i *InputItem) IsFunctionCall() bool { return i != nil && i.Type == ItemTypeFunctionCall }

// IsFunctionCallOutput reports whether the item is a tool result.
func (i *InputItem) IsFunctionCallOutput() bool {
	return i != nil && i.Type == ItemTypeFunctionCallOutput
}

// IsReasoning reports whether the item is a reasoning echo.
func (i *InputItem) IsReasoning() bool { return i != nil && i.Type == ItemTypeReasoning }

// IsMessage reports whether the output item is a message.
func (o *OutputItem) IsMessage() bool { return o != nil && o.Type == ItemTypeMessage }

// IsFunctionCall reports whether the output item is a function call.
func (o *OutputItem) IsFunctionCall() bool { return o != nil && o.Type == ItemTypeFunctionCall }

// IsReasoning reports whether the output item is a reasoning blob.
func (o *OutputItem) IsReasoning() bool { return o != nil && o.Type == ItemTypeReasoning }

// NewInputUserMessage constructs a user-role message InputItem with a
// single input_text content part. Convenience for the common case;
// callers wanting multi-part content build the InputItem directly.
func NewInputUserMessage(text string) InputItem {
	return InputItem{
		Type: ItemTypeMessage,
		Role: RoleUser,
		Content: []ContentPart{
			{Type: ContentTypeInputText, Text: text},
		},
	}
}

// NewInputDeveloperMessage constructs a developer-role message
// InputItem. Use this for system-prompt-class instructions per
// ADR-051 open question 2 (system → developer translation on
// Responses); pass system-prompt prose verbatim and let the adapter
// pick this constructor.
func NewInputDeveloperMessage(text string) InputItem {
	return InputItem{
		Type: ItemTypeMessage,
		Role: RoleDeveloper,
		Content: []ContentPart{
			{Type: ContentTypeInputText, Text: text},
		},
	}
}

// NewInputFunctionCall constructs a function_call InputItem echoing
// a prior assistant tool call. callID must match the call_id the
// model emitted; arguments is the JSON-encoded arguments string.
func NewInputFunctionCall(callID, name, arguments string) InputItem {
	return InputItem{
		Type:      ItemTypeFunctionCall,
		CallID:    callID,
		Name:      name,
		Arguments: arguments,
	}
}

// NewInputFunctionCallOutput constructs a function_call_output
// InputItem carrying a tool result. callID correlates to the prior
// function_call; output is the result body (typically JSON-encoded).
func NewInputFunctionCallOutput(callID, output string) InputItem {
	return InputItem{
		Type:   ItemTypeFunctionCallOutput,
		CallID: callID,
		Output: output,
	}
}

// NewInputReasoning constructs a reasoning InputItem echoing a prior
// response's reasoning blob. id and encryptedContent must come
// verbatim from the response item. The summary slice is optional —
// pass nil to omit.
func NewInputReasoning(id, encryptedContent string, summary []SummaryPart) InputItem {
	return InputItem{
		Type:             ItemTypeReasoning,
		ID:               id,
		EncryptedContent: encryptedContent,
		Summary:          summary,
	}
}

// OutputText returns the concatenation of output_text content parts
// on a message OutputItem. Empty for non-message items or items
// containing only refusals. Convenience helper for the common
// "assistant said X" extraction; callers needing structured
// annotations or refusal handling walk Content directly.
func (o *OutputItem) OutputText() string {
	if o == nil || !o.IsMessage() {
		return ""
	}
	var buf string
	for _, p := range o.Content {
		if p.Type == ContentTypeOutputText {
			buf += p.Text
		}
	}
	return buf
}

// RefusalText returns the first refusal content part's body on a
// message OutputItem, or "" if no refusal is present.
func (o *OutputItem) RefusalText() string {
	if o == nil || !o.IsMessage() {
		return ""
	}
	for _, p := range o.Content {
		if p.Type == ContentTypeRefusal {
			return p.Refusal
		}
	}
	return ""
}
