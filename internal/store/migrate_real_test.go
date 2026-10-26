package store

import (
	"os"
	"testing"
)

// Upgrades a copy of a real database: RUCONNECT_TEST_DB=/path/copy.sqlite
func TestMigrateRealDB(t *testing.T) {
	path := os.Getenv("RUCONNECT_TEST_DB")
	if path == "" {
		t.Skip("RUCONNECT_TEST_DB not set")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var groups, devs, grouped, strategies int
	s.db.QueryRow(`SELECT COUNT(*) FROM device_groups`).Scan(&groups)
	s.db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&devs)
	s.db.QueryRow(`SELECT COUNT(*) FROM devices d JOIN device_groups g ON g.id = d.group_id`).Scan(&grouped)
	s.db.QueryRow(`SELECT COUNT(*) FROM strategies`).Scan(&strategies)
	t.Logf("groups=%d devices=%d grouped=%d strategies=%d", groups, devs, grouped, strategies)
}
