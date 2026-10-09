package storage

import "encoding/json"

// mustMarshalJSON marshals v and returns "" instead of an error (the
// envelope was parsed from JSON, so re-marshalling cannot fail in practice).
func mustMarshalJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// marshalJSON marshals v, falling back to an empty JSON array.
func marshalJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// flattenMessage normalizes the nested signal-cli JSON envelope into the
// flat storage.Message fields. It understands the notification payload
// shapes documented in signal-cli-jsonrpc(5):
//
//   - auto receive-mode:   {"account":"+49…","envelope":{…}}
//   - manual receive-mode: {"subscription":0,"result":{"account":"+49…","envelope":{…}}}
//     (the wrapper is unwrapped before this helper runs)
//   - single-account jsonRpc: {"account":"+49…","envelope":{…}}
//
// It also keeps a flat-envelope fallback ({"source","timestamp","message",…})
// used in earlier PoC revisions of this API.
//
// The returned account field is passed through unchanged — resolving the
// account identifier (notification "account" field vs. configured client
// identity) is the caller's job.
func flattenMessage(account string, envelope map[string]interface{}) (Message, error) {
	env := envelope
	if nested, ok := envelope["envelope"].(map[string]interface{}); ok {
		env = nested
	}

	timestamp := extractInt64(env, "timestamp")
	if timestamp == 0 {
		timestamp = extractInt64(envelope, "timestamp")
	}

	sender := extractString(env, "sourceNumber")
	if sender == "" {
		sender = extractString(env, "source")
	}
	if sender == "" {
		sender = extractString(env, "sourceUuid")
	}

	sent, hasSent := syncSentMessage(env)

	recipient := ""
	if hasSent {
		recipient = extractString(sent, "destinationNumber")
		if recipient == "" {
			recipient = extractString(sent, "destination")
		}
		if recipient == "" {
			recipient = extractString(sent, "destinationUuid")
		}
	}

	groupID := ""
	if data := envelopeSection(env, "dataMessage"); data != nil {
		if gi, ok := data["groupInfo"].(map[string]interface{}); ok {
			groupID = extractString(gi, "groupId")
		}
	}
	if groupID == "" {
		groupID = extractString(envelope, "groupId")
	}

	data := envelopeSection(env, "dataMessage")
	body := ""
	rawAttachments := interface{}(nil)
	rawReaction := interface{}(nil)
	var sourceSection map[string]interface{}
	if edit, ok := env["editMessage"].(map[string]interface{}); ok {
		if d, ok := edit["dataMessage"].(map[string]interface{}); ok {
			sourceSection = d
		}
	}
	if data != nil {
		sourceSection = data
	}
	if hasSent {
		if d, ok := sent["dataMessage"].(map[string]interface{}); ok {
			sourceSection = d
		}
	}
	if sourceSection != nil {
		body = extractString(sourceSection, "message")
		rawAttachments = sourceSection["attachments"]
		if reaction, ok := sourceSection["reaction"].(map[string]interface{}); ok {
			rawReaction = reaction
		}
	} else {
		// Legacy flat envelope fallback ({"source","timestamp","message",…}).
		if body == "" {
			body = extractString(envelope, "message")
		}
		if rawAttachments == nil {
			rawAttachments = envelope["attachments"]
		}
		if rawReaction == nil {
			rawReaction = envelope["reaction"]
		}
	}

	attachments := "[]"
	if rawAttachments != nil {
		attachments = marshalJSON(rawAttachments)
	}
	reaction := ""
	if rawReaction != nil {
		reaction = marshalJSON(rawReaction)
	}

	editHistory := "[]"
	if edit, ok := env["editMessage"].(map[string]interface{}); ok {
		if ts := extractInt64(edit, "targetSentTimestamp"); ts != 0 {
			editHistory = marshalJSON([]map[string]interface{}{{"targetSentTimestamp": ts}})
		}
	}

	messageType := "text"
	if rawReaction != nil {
		messageType = "reaction"
	}
	if atts, ok := rawAttachments.([]interface{}); ok && len(atts) > 0 {
		messageType = "attachment"
	}
	if _, ok := env["editMessage"]; ok {
		messageType = "edit"
	}

	// Content-free protocol traffic (delivery/read receipts, typing or call
	// notifications, exception-only rows) has no message to persist — the
	// schema's message_type check has no type for it either. The caller
	// logs these and moves on.
	storable := sourceSection != nil ||
		hasSent ||
		body != "" ||
		rawReaction != nil
	if _, ok := env["storyMessage"]; ok {
		storable = true
	}
	if !storable {
		return Message{}, ErrNotStored
	}

	return Message{
		Account:      account,
		Timestamp:    timestamp,
		Sender:       sender,
		Recipient:    recipient,
		GroupID:      groupID,
		MessageType:  messageType,
		Body:         body,
		Attachments:  attachments,
		Reaction:     reaction,
		EditHistory:  editHistory,
		EnvelopeJSON: mustMarshalJSON(env),
	}, nil
}

// syncSentMessage returns the syncMessage.sentMessage section, which
// signal-cli emits when a message was sent from another linked device
// (mirrored outgoing messages have no sender source in the envelope).
func syncSentMessage(env map[string]interface{}) (map[string]interface{}, bool) {
	sync, ok := env["syncMessage"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	sent, ok := sync["sentMessage"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	if _, isStory := sent["storyMessage"]; isStory {
		return nil, false
	}
	return sent, true
}

func envelopeSection(env map[string]interface{}, key string) map[string]interface{} {
	section, ok := env[key].(map[string]interface{})
	if !ok {
		return nil
	}
	return section
}
