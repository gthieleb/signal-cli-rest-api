package client

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"
)

// MessageStore is the persistence sink for received signal-cli envelopes.
// Implemented by *storage.Storage; nil (= feature off) is tolerated
// everywhere — persistence must never break the receive loop.
type MessageStore interface {
	StoreMessage(account string, envelope map[string]interface{}) error
}

// envelopeAccount extracts the account identifier signal-cli puts into
// every receive notification ({"account":"+49…","envelope":{…}}; the
// manual-mode wrapper is unwrapped before this runs). Falls back to
// fallbackAccount when the notification carries no account field.
func envelopeAccount(params json.RawMessage, fallbackAccount string) (string, json.RawMessage, bool) {
	var probe struct {
		Account  string          `json:"account"`
		Envelope json.RawMessage `json:"envelope"`
	}
	if err := json.Unmarshal(params, &probe); err != nil {
		return fallbackAccount, params, false
	}
	if probe.Envelope == nil {
		// Flat envelope shapes have no "envelope" wrapper — keep raw params.
		if probe.Account == "" {
			return fallbackAccount, params, false
		}
		return probe.Account, params, true
	}
	account := probe.Account
	if account == "" {
		account = fallbackAccount
	}
	return account, probe.Envelope, true
}

// persistReceivedMessage stores one received envelope in the message
// store. Storage is a sink, not a gate: failures are logged and the
// receive loop keeps running (webhook/broadcast delivery must not
// depend on sqlite health). Mirrored outgoing transcripts may carry no
// sender — those rows keep sender="" and are skipped downstream.
func persistReceivedMessage(store MessageStore, account string, envelope json.RawMessage) {
	if store == nil {
		return
	}

	acc, envelopeJSON, ok := envelopeAccount(envelope, account)
	if !ok || len(envelopeJSON) == 0 {
		return
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(envelopeJSON, &parsed); err != nil {
		log.Error("Couldn't parse received envelope for storage: ", err.Error())
		return
	}

	if err := store.StoreMessage(acc, parsed); err != nil {
		log.Error("Couldn't store received message for account ", acc, ": ", err.Error())
	}
}
