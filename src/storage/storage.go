package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

type Storage struct {
	db *sql.DB
}

type Message struct {
	ID           int64  `json:"id"`
	Account      string `json:"account"`
	Timestamp    int64  `json:"timestamp"`
	Sender       string `json:"sender"`
	Recipient    string `json:"recipient,omitempty"`
	GroupID      string `json:"group_id,omitempty"`
	MessageType  string `json:"message_type"`
	Body         string `json:"body,omitempty"`
	Attachments  string `json:"attachments,omitempty"`
	Reaction     string `json:"reaction,omitempty"`
	EditHistory  string `json:"edit_history,omitempty"`
	EnvelopeJSON string `json:"envelope_json"`
	Read         bool   `json:"read"`
	CreatedAt    int64  `json:"created_at"`
}

func New(dataDir string) (*Storage, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	dbPath := filepath.Join(dataDir, "messages.db")
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &Storage{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		sender TEXT NOT NULL,
		recipient TEXT,
		group_id TEXT,
		message_type TEXT NOT NULL CHECK (message_type IN ('text', 'attachment', 'reaction', 'edit')),
		body TEXT,
		attachments TEXT DEFAULT '[]',
		reaction TEXT,
		edit_history TEXT DEFAULT '[]',
		envelope_json TEXT NOT NULL,
		read INTEGER DEFAULT 0,
		created_at INTEGER DEFAULT (strftime('%s', 'now'))
	) STRICT;

	CREATE INDEX IF NOT EXISTS idx_messages_account ON messages(account);
	CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
	CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender);
	CREATE INDEX IF NOT EXISTS idx_messages_group ON messages(group_id);
	`

	if _, err := db.Exec(schema); err != nil {
		return err
	}
	return nil
}

func (s *Storage) StoreMessage(account string, envelope map[string]interface{}) error {
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	timestamp := extractInt64(envelope, "timestamp")
	sender := extractString(envelope, "source")
	recipient := extractString(envelope, "sourceDevice")
	groupID := extractString(envelope, "groupId")
	body := extractString(envelope, "message")
	
	messageType := "text"
	if _, hasAttachments := envelope["attachments"]; hasAttachments {
		messageType = "attachment"
	}
	if _, hasReaction := envelope["reaction"]; hasReaction {
		messageType = "reaction"
	}

	attachments := "[]"
	if atts, ok := envelope["attachments"]; ok {
		if attsJSON, err := json.Marshal(atts); err == nil {
			attachments = string(attsJSON)
		}
	}

	reaction := ""
	if react, ok := envelope["reaction"]; ok {
		if reactJSON, err := json.Marshal(react); err == nil {
			reaction = string(reactJSON)
		}
	}

	_, err = s.db.Exec(`
		INSERT INTO messages (account, timestamp, sender, recipient, group_id, message_type, body, attachments, reaction, envelope_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, account, timestamp, sender, recipient, groupID, messageType, body, attachments, reaction, string(envelopeJSON))

	if err != nil {
		return fmt.Errorf("failed to store message: %w", err)
	}
	return nil
}

func (s *Storage) GetMessages(account string, limit, offset int, filters map[string]interface{}) ([]Message, int, error) {
	whereClause := "account = ?"
	args := []interface{}{account}

	if since, ok := filters["since"]; ok {
		whereClause += " AND timestamp >= ?"
		args = append(args, since)
	}
	if until, ok := filters["until"]; ok {
		whereClause += " AND timestamp <= ?"
		args = append(args, until)
	}
	if sender, ok := filters["sender"]; ok {
		whereClause += " AND sender = ?"
		args = append(args, sender)
	}
	if group, ok := filters["group"]; ok {
		whereClause += " AND group_id = ?"
		args = append(args, group)
	}
	if msgType, ok := filters["type"]; ok {
		whereClause += " AND message_type = ?"
		args = append(args, msgType)
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM messages WHERE %s", whereClause)
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count messages: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, account, timestamp, sender, recipient, group_id, message_type, body, attachments, reaction, edit_history, envelope_json, read, created_at
		FROM messages
		WHERE %s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, whereClause)
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var readInt int
		err := rows.Scan(
			&msg.ID, &msg.Account, &msg.Timestamp, &msg.Sender, &msg.Recipient,
			&msg.GroupID, &msg.MessageType, &msg.Body, &msg.Attachments,
			&msg.Reaction, &msg.EditHistory, &msg.EnvelopeJSON, &readInt, &msg.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan message: %w", err)
		}
		msg.Read = readInt != 0
		messages = append(messages, msg)
	}

	return messages, total, nil
}

func (s *Storage) GetMessage(account string, messageID int64) (*Message, error) {
	var msg Message
	var readInt int
	err := s.db.QueryRow(`
		SELECT id, account, timestamp, sender, recipient, group_id, message_type, body, attachments, reaction, edit_history, envelope_json, read, created_at
		FROM messages
		WHERE account = ? AND id = ?
	`, account, messageID).Scan(
		&msg.ID, &msg.Account, &msg.Timestamp, &msg.Sender, &msg.Recipient,
		&msg.GroupID, &msg.MessageType, &msg.Body, &msg.Attachments,
		&msg.Reaction, &msg.EditHistory, &msg.EnvelopeJSON, &readInt, &msg.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	msg.Read = readInt != 0
	return &msg, nil
}

func (s *Storage) DeleteMessage(account string, messageID int64) error {
	result, err := s.db.Exec("DELETE FROM messages WHERE account = ? AND id = ?", account, messageID)
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("message not found")
	}
	return nil
}

func (s *Storage) MarkAsRead(account string, messageID int64) error {
	_, err := s.db.Exec("UPDATE messages SET read = 1 WHERE account = ? AND id = ?", account, messageID)
	if err != nil {
		return fmt.Errorf("failed to mark message as read: %w", err)
	}
	return nil
}

func (s *Storage) Close() error {
	return s.db.Close()
}

func extractString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func extractInt64(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int64:
			return val
		case float64:
			return int64(val)
		case int:
			return int64(val)
		}
	}
	return 0
}
