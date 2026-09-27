package serverstore

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/picoaide/picoaide/internal/util"
)

const maxTranscriptRequestBytes = 64 << 20

var ErrLLMTranscriptResponseTooLarge = errors.New("transcript response exceeds audit limit")

type LLMTranscript struct {
	ID            int64      `json:"id"`
	RequestID     string     `json:"request_id"`
	UserID        int64      `json:"user_id"`
	Endpoint      string     `json:"endpoint"`
	Model         string     `json:"model"`
	RequestBody   string     `json:"request_body,omitempty"`
	StatusCode    int        `json:"status_code"`
	ResponseBytes int64      `json:"response_bytes"`
	ResponseSHA   string     `json:"response_sha256"`
	AuditStatus   string     `json:"audit_status"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

type LLMTranscriptPage struct {
	Items []LLMTranscript
	Total int64
}

type LLMTranscriptFilter struct {
	UserID    int64
	Model     string
	Endpoint  string
	CreatedAt *time.Time
}

func NewTranscriptRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func CreateLLMTranscript(db *sql.DB, userID int64, endpoint, model string, requestBody []byte) (int64, string, error) {
	if len(requestBody) > maxTranscriptRequestBytes {
		return 0, "", fmt.Errorf("transcript request exceeds %d bytes", maxTranscriptRequestBytes)
	}
	key, err := util.GetMasterKey()
	if err != nil {
		return 0, "", err
	}
	requestID, err := NewTranscriptRequestID()
	if err != nil {
		return 0, "", err
	}
	ciphertext := util.Encrypt(key, string(requestBody))
	var id int64
	err = db.QueryRow(`INSERT INTO llm_transcripts
		(request_id, user_id, endpoint, model, request_body_enc)
		VALUES (?, ?, ?, ?, ?) RETURNING id`, requestID, userID, endpoint, model, ciphertext).Scan(&id)
	return id, requestID, err
}

func AppendLLMTranscriptChunk(db *sql.DB, transcriptID, seq int64, payload []byte) error {
	if len(payload) == 0 {
		return nil
	}
	key, err := util.GetMasterKey()
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO llm_transcript_chunks
		(transcript_id, seq, payload_enc, byte_length)
		VALUES (?, ?, ?, ?)`, transcriptID, seq, util.Encrypt(key, string(payload)), len(payload))
	return err
}

func FinishLLMTranscript(db *sql.DB, transcriptID int64, statusCode int, responseBytes int64, responseSHA, auditStatus string) error {
	if auditStatus == "" {
		auditStatus = "complete"
	}
	_, err := db.Exec(`UPDATE llm_transcripts SET status_code=?, response_bytes=?,
		response_sha256=?, audit_status=?, completed_at=now() WHERE id=?`,
		statusCode, responseBytes, responseSHA, auditStatus, transcriptID)
	return err
}

func GetLLMTranscript(db *sql.DB, id int64) (LLMTranscript, error) {
	var row LLMTranscript
	var requestBodyEnc string
	var completed sql.NullTime
	err := db.QueryRow(`SELECT id, request_id, user_id, endpoint, model,
		request_body_enc, status_code, response_bytes, response_sha256,
		audit_status, created_at, completed_at FROM llm_transcripts WHERE id=?`, id).
		Scan(&row.ID, &row.RequestID, &row.UserID, &row.Endpoint, &row.Model,
			&requestBodyEnc, &row.StatusCode, &row.ResponseBytes, &row.ResponseSHA,
			&row.AuditStatus, &row.CreatedAt, &completed)
	if err != nil {
		return LLMTranscript{}, err
	}
	key, err := util.GetMasterKey()
	if err != nil {
		return LLMTranscript{}, err
	}
	row.RequestBody, err = util.Decrypt(key, requestBodyEnc)
	if err != nil {
		return LLMTranscript{}, err
	}
	if completed.Valid {
		row.CompletedAt = &completed.Time
	}
	return row, nil
}

func ReadLLMTranscriptResponse(db *sql.DB, transcriptID int64) ([]byte, error) {
	key, err := util.GetMasterKey()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT payload_enc FROM llm_transcript_chunks
		WHERE transcript_id=? ORDER BY seq`, transcriptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var response []byte
	for rows.Next() {
		var payloadEnc string
		if err := rows.Scan(&payloadEnc); err != nil {
			return nil, err
		}
		payload, err := util.Decrypt(key, payloadEnc)
		if err != nil {
			return nil, err
		}
		response = append(response, payload...)
	}
	return response, rows.Err()
}

// PurgeOldLLMTranscripts removes encrypted prompt/response records and their
// chunks. The foreign key cascade makes the deletion atomic from the caller's
// perspective and prevents orphaned encrypted chunks.
func PurgeOldLLMTranscripts(db *sql.DB, cutoff time.Time) (int64, error) {
	result, err := db.Exec(`DELETE FROM llm_transcripts WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func ListLLMTranscripts(db *sql.DB, userID, offset, limit int64, model string) (LLMTranscriptPage, error) {
	return ListLLMTranscriptsFiltered(db, LLMTranscriptFilter{UserID: userID, Model: model}, offset, limit)
}

func ListLLMTranscriptsFiltered(db *sql.DB, filter LLMTranscriptFilter, offset, limit int64) (LLMTranscriptPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	where := ""
	args := []any{}
	add := func(clause string, value any) {
		if where == "" {
			where = " WHERE " + clause
		} else {
			where += " AND " + clause
		}
		args = append(args, value)
	}
	if filter.UserID > 0 {
		add("user_id=?", filter.UserID)
	}
	if filter.Model != "" {
		add("model=?", filter.Model)
	}
	if filter.Endpoint != "" {
		add("endpoint=?", filter.Endpoint)
	}
	if filter.CreatedAt != nil {
		add("created_at >= ?", *filter.CreatedAt)
	}
	var page LLMTranscriptPage
	if err := db.QueryRow("SELECT COUNT(*) FROM llm_transcripts"+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	args = append(args, limit, offset)
	rows, err := db.Query(`SELECT id, request_id, user_id, endpoint, model,
		status_code, response_bytes, response_sha256, audit_status, created_at, completed_at
		FROM llm_transcripts`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var row LLMTranscript
		var completed sql.NullTime
		if err := rows.Scan(&row.ID, &row.RequestID, &row.UserID, &row.Endpoint, &row.Model,
			&row.StatusCode, &row.ResponseBytes, &row.ResponseSHA, &row.AuditStatus,
			&row.CreatedAt, &completed); err != nil {
			return page, err
		}
		if completed.Valid {
			row.CompletedAt = &completed.Time
		}
		page.Items = append(page.Items, row)
	}
	return page, rows.Err()
}

func TranscriptModelFromRequest(body []byte) string {
	var value struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &value) != nil {
		return ""
	}
	return value.Model
}

func TranscriptResponseHash(body []byte) string {
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func TranscriptHashHex(sum []byte) string {
	return hex.EncodeToString(sum)
}

var ErrTranscriptNotFound = errors.New("llm transcript not found")
