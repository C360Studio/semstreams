package wire

import "testing"

func TestDecodeError_StandardEnvelope(t *testing.T) {
	t.Parallel()
	body := []byte(`{"error":{"message":"bad input","type":"invalid_request_error","code":"missing_field","param":"messages"}}`)
	err := DecodeError(400, body)
	if err.StatusCode != 400 || err.Message != "bad input" || err.Type != "invalid_request_error" || err.Code != "missing_field" || err.Param != "messages" {
		t.Errorf("decoded shape wrong: %+v", err)
	}
}

func TestDecodeError_ArrayWrapped(t *testing.T) {
	t.Parallel()
	body := []byte(`[{"error":{"code":400,"message":"thought_signature missing","status":"INVALID_ARGUMENT"}}]`)
	err := DecodeError(400, body)
	if err.StatusCode != 400 || err.Message != "thought_signature missing" {
		t.Errorf("array-wrapped decode wrong: %+v", err)
	}
}

func TestDecodeError_LeadingWhitespace(t *testing.T) {
	t.Parallel()
	body := []byte("\n\t  [{\"error\":{\"message\":\"x\"}}]\n")
	err := DecodeError(500, body)
	if err.Message != "x" {
		t.Errorf("whitespace-tolerant decode wrong: %+v", err)
	}
}

func TestDecodeError_BareErrorObject(t *testing.T) {
	t.Parallel()
	body := []byte(`{"message":"flat","type":"x","code":"y"}`)
	err := DecodeError(403, body)
	if err.StatusCode != 403 || err.Message != "flat" {
		t.Errorf("bare-object decode wrong: %+v", err)
	}
}

func TestDecodeError_EmptyBody(t *testing.T) {
	t.Parallel()
	err := DecodeError(502, nil)
	if err.StatusCode != 502 {
		t.Errorf("empty-body status drift: %+v", err)
	}
	if err.Message == "" {
		t.Errorf("empty-body message empty: %+v", err)
	}
}

func TestDecodeError_Garbage(t *testing.T) {
	t.Parallel()
	body := []byte(`<html><body>503 Service Unavailable</body></html>`)
	err := DecodeError(503, body)
	if err.StatusCode != 503 {
		t.Errorf("garbage-body status drift: %+v", err)
	}
	if err.Message == "" {
		t.Errorf("garbage-body message empty: %+v", err)
	}
}

func TestAPIError_ErrorString(t *testing.T) {
	t.Parallel()
	e := &APIError{StatusCode: 429, Type: "rate_limit_exceeded", Code: "rate_limit", Message: "slow down"}
	got := e.Error()
	if got == "" {
		t.Fatal("Error() empty")
	}
}
