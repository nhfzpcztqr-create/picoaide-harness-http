package serverstore

import (
	"database/sql"
	"errors"
	"time"
)

type GatewayProviderAPIKey struct {
	ID            int64      `json:"id"`
	ProviderID    int64      `json:"provider_id"`
	APIKeyEnc     string     `json:"-"`
	Label         string     `json:"label"`
	Enabled       bool       `json:"enabled"`
	Priority      int        `json:"priority"`
	CooldownUntil *time.Time `json:"cooldown_until,omitempty"`
	FailureCount  int        `json:"failure_count"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
}

const providerAPIKeyColumns = `id, provider_id, api_key_enc, label, enabled, priority, cooldown_until, failure_count, last_used_at, last_error_at`

func scanGatewayProviderAPIKey(scan interface{ Scan(...any) error }) (*GatewayProviderAPIKey, error) {
	var key GatewayProviderAPIKey
	if err := scan.Scan(&key.ID, &key.ProviderID, &key.APIKeyEnc, &key.Label, &key.Enabled, &key.Priority, &key.CooldownUntil, &key.FailureCount, &key.LastUsedAt, &key.LastErrorAt); err != nil {
		return nil, err
	}
	return &key, nil
}

func ListGatewayProviderAPIKeys(db *sql.DB, providerID int64) ([]GatewayProviderAPIKey, error) {
	rows, err := db.Query(`SELECT `+providerAPIKeyColumns+` FROM gateway_provider_api_keys WHERE provider_id = ? ORDER BY priority, id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayProviderAPIKey
	for rows.Next() {
		key, err := scanGatewayProviderAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *key)
	}
	return out, rows.Err()
}

func GetGatewayProviderAPIKey(db *sql.DB, providerID, keyID int64) (*GatewayProviderAPIKey, error) {
	key, err := scanGatewayProviderAPIKey(db.QueryRow(`SELECT `+providerAPIKeyColumns+` FROM gateway_provider_api_keys WHERE provider_id = ? AND id = ?`, providerID, keyID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return key, err
}

func AddGatewayProviderAPIKey(db *sql.DB, key *GatewayProviderAPIKey) (int64, error) {
	id, err := InsertID(db, `INSERT INTO gateway_provider_api_keys (provider_id, api_key_enc, label, enabled, priority) VALUES (?, ?, ?, ?, ?)`, key.ProviderID, key.APIKeyEnc, key.Label, key.Enabled, key.Priority)
	if err != nil {
		return 0, err
	}
	key.ID = id
	return id, nil
}

func UpdateGatewayProviderAPIKey(db *sql.DB, key *GatewayProviderAPIKey) error {
	result, err := db.Exec(`UPDATE gateway_provider_api_keys SET api_key_enc = ?, label = ?, enabled = ?, priority = ?, updated_at = now() WHERE provider_id = ? AND id = ?`, key.APIKeyEnc, key.Label, key.Enabled, key.Priority, key.ProviderID, key.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func DeleteGatewayProviderAPIKey(db *sql.DB, providerID, keyID int64) error {
	result, err := db.Exec(`DELETE FROM gateway_provider_api_keys WHERE provider_id = ? AND id = ?`, providerID, keyID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func ResetGatewayProviderAPIKey(db *sql.DB, providerID, keyID int64) error {
	result, err := db.Exec(`UPDATE gateway_provider_api_keys SET cooldown_until = NULL, failure_count = 0, last_error_at = NULL, updated_at = now() WHERE provider_id = ? AND id = ?`, providerID, keyID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
