package client

import (
	"encoding/json"
	"testing"
)

type recordingStore struct {
	accounts  []string
	envelopes []map[string]interface{}
	fail      bool
}

func (r *recordingStore) StoreMessage(account string, envelope map[string]interface{}) error {
	if r.fail {
		return json.Unmarshal(nil, &struct{}{})
	}
	r.accounts = append(r.accounts, account)
	r.envelopes = append(r.envelopes, envelope)
	return nil
}

func Test_EnvelopeAccount_NestedNotification(t *testing.T) {
	params := []byte(`{"account":"+49123456789","envelope":{"source":"+491112222","timestamp":1631458508784}}`)
	account, envelope, ok := envelopeAccount(params, "<multi-account>")
	if !ok {
		t.Fatal("expected notification to be recognized")
	}
	if account != "+49123456789" {
		t.Errorf("got account %q, wanted %q (notification account field wins)", account, "+49123456789")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(envelope, &parsed); err != nil {
		t.Fatalf("envelope not json: %s", err)
	}
	if _, hasSource := parsed["source"]; !hasSource {
		t.Errorf("expected unwrapped inner envelope, got %s", string(envelope))
	}
}

func Test_EnvelopeAccount_FallsBackToClientNumber(t *testing.T) {
	params := []byte(`{"envelope":{"source":"+491112222"}}`)
	account, _, ok := envelopeAccount(params, "+495555555")
	if !ok {
		t.Fatal("expected notification to be recognized")
	}
	if account != "+495555555" {
		t.Errorf("got account %q, wanted fallback %q", account, "+495555555")
	}
}

func Test_EnvelopeAccount_UnparsableFallsBack(t *testing.T) {
	account, _, ok := envelopeAccount([]byte(`not json`), "<multi-account>")
	if ok {
		t.Error("unparsable payload should report ok=false")
	}
	if account != "<multi-account>" {
		t.Errorf("expected fallback account, got %q", account)
	}
}

func Test_PersistReceivedMessage_NilStoreIsNoOp(t *testing.T) {
	// must not panic / block when persistence is disabled
	persistReceivedMessage(nil, "+49123456789", []byte(`{"account":"+49123456789","envelope":{}}`))
}

func Test_PersistReceivedMessage_Roundtrip(t *testing.T) {
	store := &recordingStore{}
	params := []byte(`{"account":"+49123456789","envelope":{"source":"+491112222","sourceNumber":"+491112222","timestamp":1631458508784,"dataMessage":{"message":"foobar"}}}`)
	persistReceivedMessage(store, "<multi-account>", params)

	if len(store.accounts) != 1 {
		t.Fatalf("expected 1 stored message, got %d", len(store.accounts))
	}
	if store.accounts[0] != "+49123456789" {
		t.Errorf("account passed to store = %q, wanted %q (the registered number, not the client config key)", store.accounts[0], "+49123456789")
	}
	env := store.envelopes[0]
	if env["sourceNumber"] != "+491112222" {
		t.Errorf("stored envelope lost the payload: %v", env)
	}
}

func Test_PersistReceivedMessage_StoreFailureDoesNotPanic(t *testing.T) {
	store := &recordingStore{fail: true}
	params := []byte(`{"account":"+49123456789","envelope":{"sourceNumber":"+491112222","dataMessage":{"message":"x"}}}`)
	persistReceivedMessage(store, "+49123456789", params)
	if len(store.accounts) != 0 {
		t.Error("failed store should not record entries")
	}
}

func Test_ManualReceiveMode_Env(t *testing.T) {
	c := NewJsonRpc2Client(nil, "<multi-account>")
	if c.manualReceiveMode() {
		t.Error("default (no env set) must not be manual")
	}

	t.Setenv("JSON_RPC_RECEIVE_MODE", "manual")
	if !c.manualReceiveMode() {
		t.Error("JSON_RPC_RECEIVE_MODE=manual must enable manual mode")
	}
}

func Test_ManualReceiveMode_OverrideSeam(t *testing.T) {
	c := NewJsonRpc2Client(nil, "<multi-account>")
	c.setManualReceiveMode(true)
	if !c.manualReceiveMode() {
		t.Error("override true must win")
	}
	c.setManualReceiveMode(false)
	if c.manualReceiveMode() {
		t.Error("override false must win")
	}
}

func Test_BaseSubscriptionRefCount_NeverReleased(t *testing.T) {
	// The floor guarantees a boot-time subscription is never torn down by
	// websocket refcount churn.
	if baseSubscriptionRefCount != 1 {
		t.Errorf("baseSubscriptionRefCount = %d, wanted 1", baseSubscriptionRefCount)
	}
}
