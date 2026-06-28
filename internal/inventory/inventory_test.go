package inventory

import (
	"testing"

	"xenon/internal/content"
	"xenon/internal/model"
	"xenon/internal/persist"
)

// TestPersistenceSurvivesRestart is the core guarantee: a device onboarded via the
// GUI is written to SQLite and reloaded on the next start (no re-onboarding, no
// duplicate seeds).
func TestPersistenceSurvivesRestart(t *testing.T) {
	c, err := content.LoadBundled()
	if err != nil {
		t.Fatal(err)
	}
	dbPath := t.TempDir() + "/xenon.db"

	// First boot: empty DB -> seeds inserted.
	db, err := persist.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(c, db)
	if err != nil {
		t.Fatal(err)
	}
	seeded := len(s.List())
	if seeded == 0 {
		t.Fatal("expected example devices to be seeded on first run")
	}

	// Onboard a new device through the same path the GUI uses.
	o := s.Preview(model.Signature{Vendor: "Juniper", Model: "EX4100-F-12P", OS: "Junos"}, "newsw", "10.0.0.9:50051")
	if err := s.Add(o); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Second boot (simulated restart): same DB, no re-seed, onboarded device present.
	db2, err := persist.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	s2, err := NewStore(c, db2)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s2.List()); got != seeded+1 {
		t.Fatalf("after restart: got %d devices, want %d (seeds must not duplicate)", got, seeded+1)
	}
	if _, ok := s2.Get("d-newsw"); !ok {
		t.Fatal("onboarded device did not survive restart")
	}
}
