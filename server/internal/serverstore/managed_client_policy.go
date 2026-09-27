package serverstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type ManagedSkillPolicy struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Version  string `json:"version"`
	Revision int64  `json:"revision"`
}

type ManagedClientDevice struct {
	DeviceID        string    `json:"device_id"`
	Platform        string    `json:"platform"`
	ClientVersion   string    `json:"client_version"`
	Inventory       []any     `json:"inventory"`
	AppliedRevision int64     `json:"applied_revision"`
	SyncStatus      string    `json:"sync_status"`
	SyncError       string    `json:"sync_error,omitempty"`
	LastSeenAt      time.Time `json:"last_seen_at"`
}

type ManagedUserConfig struct {
	UserID   int64                 `json:"user_id"`
	Settings map[string]any        `json:"settings"`
	Revision int64                 `json:"revision"`
	Skills   []ManagedSkillPolicy  `json:"skills"`
	Devices  []ManagedClientDevice `json:"devices"`
}

func GetManagedUserConfig(db *sql.DB, userID int64) (*ManagedUserConfig, error) {
	if db == nil {
		return nil, errors.New("database is nil")
	}
	out := &ManagedUserConfig{UserID: userID, Settings: map[string]any{}, Skills: []ManagedSkillPolicy{}, Devices: []ManagedClientDevice{}}
	var raw string
	if err := db.QueryRow("SELECT settings_json, revision FROM managed_user_configs WHERE user_id = ?", userID).Scan(&raw, &out.Revision); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		out.Revision = 0
	} else if err := json.Unmarshal([]byte(raw), &out.Settings); err != nil {
		return nil, fmt.Errorf("decode managed settings: %w", err)
	}
	if out.Settings == nil {
		out.Settings = map[string]any{}
	}

	rows, err := db.Query("SELECT skill_name, mode, version, revision FROM managed_skill_policies WHERE user_id = ? ORDER BY skill_name", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ManagedSkillPolicy
		if err := rows.Scan(&p.Name, &p.Mode, &p.Version, &p.Revision); err != nil {
			return nil, err
		}
		out.Skills = append(out.Skills, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	deviceRows, err := db.Query("SELECT device_id, platform, client_version, inventory_json, applied_revision, sync_status, sync_error, last_seen_at FROM managed_client_devices WHERE user_id = ? ORDER BY last_seen_at DESC", userID)
	if err != nil {
		return nil, err
	}
	defer deviceRows.Close()
	for deviceRows.Next() {
		var d ManagedClientDevice
		var rawInventory string
		if err := deviceRows.Scan(&d.DeviceID, &d.Platform, &d.ClientVersion, &rawInventory, &d.AppliedRevision, &d.SyncStatus, &d.SyncError, &d.LastSeenAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(rawInventory), &d.Inventory); err != nil {
			return nil, fmt.Errorf("decode device inventory: %w", err)
		}
		if d.Inventory == nil {
			d.Inventory = []any{}
		}
		out.Devices = append(out.Devices, d)
	}
	if err := deviceRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func SaveManagedUserConfig(db *sql.DB, userID int64, settings map[string]any, skills []ManagedSkillPolicy, updatedBy int64) (int64, error) {
	if db == nil {
		return 0, errors.New("database is nil")
	}
	if settings == nil {
		settings = map[string]any{}
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return 0, fmt.Errorf("encode managed settings: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRow(`
		INSERT INTO managed_user_configs (user_id, settings_json, revision, updated_by, updated_at)
		VALUES (?, ?, 1, ?, now())
		ON CONFLICT (user_id) DO UPDATE SET
		  settings_json = EXCLUDED.settings_json,
		  revision = managed_user_configs.revision + 1,
		  updated_by = EXCLUDED.updated_by,
		  updated_at = now()
		RETURNING revision`, userID, string(raw), updatedBy).Scan(&revision)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM managed_skill_policies WHERE user_id = ?", userID); err != nil {
		return 0, err
	}
	for _, skill := range skills {
		if _, err := tx.Exec(`
			INSERT INTO managed_skill_policies (user_id, skill_name, mode, version, revision, updated_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, now())`, userID, skill.Name, skill.Mode, skill.Version, revision, updatedBy); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func ReportManagedClientDevice(db *sql.DB, userID int64, device ManagedClientDevice) error {
	if db == nil {
		return errors.New("database is nil")
	}
	if device.Inventory == nil {
		device.Inventory = []any{}
	}
	raw, err := json.Marshal(device.Inventory)
	if err != nil {
		return fmt.Errorf("encode device inventory: %w", err)
	}
	_, err = db.Exec(`
		INSERT INTO managed_client_devices
		  (user_id, device_id, platform, client_version, inventory_json, applied_revision, sync_status, sync_error, last_seen_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, now(), now())
		ON CONFLICT (user_id, device_id) DO UPDATE SET
		  platform = EXCLUDED.platform,
		  client_version = EXCLUDED.client_version,
		  inventory_json = EXCLUDED.inventory_json,
		  applied_revision = EXCLUDED.applied_revision,
		  sync_status = EXCLUDED.sync_status,
		  sync_error = EXCLUDED.sync_error,
		  last_seen_at = now(),
		  updated_at = now()`, userID, device.DeviceID, device.Platform, device.ClientVersion, string(raw), device.AppliedRevision, device.SyncStatus, device.SyncError)
	return err
}
