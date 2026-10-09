package storage

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatalf("storage.New: %s", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// auto-mode notification shape (signal-cli-jsonrpc(5)): the daemon pushes
// {"account":…,"envelope":{…}} on the json-rpc connection; persistReceivedMessage
// feeds the inner envelope map into StoreMessage.
var dataMessageEnvelope = map[string]interface{}{
	"account": "+49123456789",
	"envelope": map[string]interface{}{
		"source":       "+491112222",
		"sourceNumber": "+491112222",
		"sourceUuid":   "11111111-2222-3333-4444-555555555555",
		"sourceName":   "Erika",
		"sourceDevice": float64(2),
		"timestamp":    float64(1631458508784),
		"dataMessage": map[string]interface{}{
			"timestamp":        float64(1631458508784),
			"message":          "foobar",
			"expiresInSeconds": float64(0),
			"attachments":      []interface{}{},
		},
	},
}

func Test_StoreMessage_Roundtrip_NestedDataMessage(t *testing.T) {
	s := newTestStorage(t)

	if err := s.StoreMessage("+49123456789", dataMessageEnvelope); err != nil {
		t.Fatalf("StoreMessage: %s", err)
	}

	messages, total, err := s.GetMessages("+49123456789", 100, 0, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetMessages: %s", err)
	}
	if total != 1 || len(messages) != 1 {
		t.Fatalf("total=%d len=%d, wanted 1/1", total, len(messages))
	}

	msg := messages[0]
	if msg.Sender != "+491112222" {
		t.Errorf("sender = %q, wanted envelope sourceNumber", msg.Sender)
	}
	if msg.Body != "foobar" {
		t.Errorf("body = %q, wanted dataMessage.message", msg.Body)
	}
	if msg.MessageType != "text" {
		t.Errorf("message type = %q, wanted text", msg.MessageType)
	}
	if msg.Timestamp != 1631458508784 {
		t.Errorf("timestamp = %d, wanted envelope timestamp (millis)", msg.Timestamp)
	}
	if msg.Account != "+49123456789" {
		t.Errorf("account = %q, wanted the account passed by the caller", msg.Account)
	}
	var storedEnvelope map[string]interface{}
	if err := json.Unmarshal([]byte(msg.EnvelopeJSON), &storedEnvelope); err != nil {
		t.Fatalf("envelope_json not json: %s", err)
	}
	if _, ok := storedEnvelope["dataMessage"]; !ok {
		t.Errorf("envelope_json should hold the inner envelope, got %s", msg.EnvelopeJSON)
	}
}

func Test_StoreMessage_Roundtrip_ManualWrapper(t *testing.T) {
	// The receive loop (client package) unwraps
	// {"subscription":N,"result":{…}} and extracts the account via the
	// notification's "account" field before calling StoreMessage. Mirror
	// that sequence here without importing the client package.
	wrapper := struct {
		Subscription int64           `json:"subscription"`
		Result       json.RawMessage `json:"result"`
	}{}
	if err := json.Unmarshal(json.RawMessage(`{"subscription":0,"result":{"account":"+49123456789","envelope":{"sourceNumber":"+491112222","timestamp":1631458508785,"dataMessage":{"message":"manual"}}}}`), &wrapper); err != nil {
		t.Fatalf("unmarshal wrapper: %s", err)
	}

	probe := struct {
		Account  string          `json:"account"`
		Envelope json.RawMessage `json:"envelope"`
	}{}
	if err := json.Unmarshal(wrapper.Result, &probe); err != nil {
		t.Fatalf("unmarshal result: %s", err)
	}
	if probe.Account != "+49123456789" {
		t.Fatalf("account extraction mismatch: %q", probe.Account)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(probe.Envelope, &parsed); err != nil {
		t.Fatalf("unmarshal envelope: %s", err)
	}

	s := newTestStorage(t)
	if err := s.StoreMessage(probe.Account, parsed); err != nil {
		t.Fatalf("StoreMessage: %s", err)
	}

	messages, _, err := s.GetMessages("+49123456789", 100, 0, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetMessages: %s", err)
	}
	if len(messages) != 1 || messages[0].Body != "manual" {
		t.Fatalf("manual-mode roundtrip failed: %+v", messages)
	}
}

func Test_StoreMessage_ReceiptOnlyEnvelopeIsSkipped(t *testing.T) {
	s := newTestStorage(t)

	envelope := map[string]interface{}{
		"account": "+49123456789",
		"envelope": map[string]interface{}{
			"source":         "+491112222",
			"sourceDevice":   float64(1),
			"timestamp":      float64(1631458508786),
			"receiptMessage": map[string]interface{}{"type": "DELIVERY"},
		},
	}
	if err := s.StoreMessage("+49123456789", envelope); err != nil {
		t.Fatalf("receipt-only envelope must be a silent no-op, got error: %s", err)
	}

	_, total, err := s.GetMessages("+49123456789", 100, 0, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetMessages: %s", err)
	}
	if total != 0 {
		t.Errorf("total = %d, wanted 0 (no storable content)", total)
	}

	if !errors.Is(ErrNotStored, ErrNotStored) {
		t.Error("ErrNotStored sentinel missing")
	}
}

func Test_StoreMessage_LegacyFlatEnvelope(t *testing.T) {
	s := newTestStorage(t)

	envelope := map[string]interface{}{
		"source":       "+491112222",
		"timestamp":    float64(1631458508787),
		"message":      "legacy body",
		"sourceDevice": float64(1),
	}
	if err := s.StoreMessage("+49123456789", envelope); err != nil {
		t.Fatalf("StoreMessage: %s", err)
	}

	messages, total, err := s.GetMessages("+49123456789", 100, 0, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetMessages: %s", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, wanted 1", total)
	}
	if messages[0].Body != "legacy body" {
		t.Errorf("body = %q, wanted legacy fallback", messages[0].Body)
	}
}

func Test_StoreMessage_GroupMessageKeepsGroupId(t *testing.T) {
	s := newTestStorage(t)

	envelope := map[string]interface{}{
		"account": "+49123456789",
		"envelope": map[string]interface{}{
			"source":    "+491112222",
			"timestamp": float64(1631458508788),
			"dataMessage": map[string]interface{}{
				"message": "group hello",
				"groupInfo": map[string]interface{}{
					"groupId": "BASE64GROUPID==",
				},
			},
		},
	}
	if err := s.StoreMessage("+49123456789", envelope); err != nil {
		t.Fatalf("StoreMessage: %s", err)
	}

	messages, _, err := s.GetMessages("+49123456789", 100, 0, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetMessages: %s", err)
	}
	if len(messages) != 1 || messages[0].GroupID != "BASE64GROUPID==" {
		t.Fatalf("group id not persisted: %+v", messages)
	}
}

func Test_StoreMessage_GetMessagesFilterIsolation(t *testing.T) {
	s := newTestStorage(t)

	if err := s.StoreMessage("+49123456789", dataMessageEnvelope); err != nil {
		t.Fatalf("StoreMessage: %s", err)
	}

	// The gateway polls by registered number — rows for other accounts
	// (or a mis-keyed sentinel account) must not leak into the result.
	_, total, err := s.GetMessages("<multi-account>", 100, 0, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetMessages: %s", err)
	}
	if total != 0 {
		t.Errorf("other-account query returned %d rows, wanted 0", total)
	}
}
