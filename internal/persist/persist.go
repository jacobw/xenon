// Package persist stores the M1 inventory in an embedded SQLite database, the
// norm for self-hosted single-binary control planes (lldap, Grafana, Authelia all
// default to SQLite on a small volume). The pure-Go modernc.org/sqlite driver
// keeps xenond a static CGO_ENABLED=0 binary on distroless.
//
// Only onboarding *inputs* are persisted (signature + opt-ins + tags); the engine
// re-derives detection, profile and compiled config on load, so the DB never holds
// stale derived state.
package persist

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"xenon/internal/model"
)

// DeviceRecord is one device's persisted onboarding input.
type DeviceRecord struct {
	Name   string
	Mgmt   string
	Sig    model.Signature
	OptIns []string
	Tags   map[string]string
}

// Store is a SQLite-backed device store.
type Store struct{ db *sql.DB }

// Open opens (creating if needed) the SQLite database at path and ensures schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite: serialize writers, avoid "database is locked"
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS devices (
		name   TEXT PRIMARY KEY,
		mgmt   TEXT NOT NULL,
		sig    TEXT NOT NULL,
		optins TEXT NOT NULL DEFAULT '[]',
		tags   TEXT NOT NULL DEFAULT '{}'
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Count returns the number of stored devices.
func (s *Store) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&n)
	return n, err
}

// List returns all stored device records ordered by name.
func (s *Store) List() ([]DeviceRecord, error) {
	rows, err := s.db.Query(`SELECT name, mgmt, sig, optins, tags FROM devices ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeviceRecord
	for rows.Next() {
		var r DeviceRecord
		var sig, optins, tags string
		if err := rows.Scan(&r.Name, &r.Mgmt, &sig, &optins, &tags); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(sig), &r.Sig); err != nil {
			return nil, fmt.Errorf("decode sig for %s: %w", r.Name, err)
		}
		_ = json.Unmarshal([]byte(optins), &r.OptIns)
		_ = json.Unmarshal([]byte(tags), &r.Tags)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Upsert inserts or updates a device record by name.
func (s *Store) Upsert(r DeviceRecord) error {
	sig, err := json.Marshal(r.Sig)
	if err != nil {
		return err
	}
	optins, _ := json.Marshal(r.OptIns)
	tags, _ := json.Marshal(r.Tags)
	_, err = s.db.Exec(`INSERT INTO devices(name, mgmt, sig, optins, tags) VALUES(?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET mgmt=excluded.mgmt, sig=excluded.sig, optins=excluded.optins, tags=excluded.tags`,
		r.Name, r.Mgmt, string(sig), string(optins), string(tags))
	return err
}
