package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func openUnversionedStore(t *testing.T, path string) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	execMigrationTest(t, db, schemaV1)
	return &Store{db: db}
}

func execMigrationTest(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func migrationTestCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil || count != want {
		t.Fatalf("%s = %d, %v; want %d", query, count, err, want)
	}
}

func TestMigrationsOnFreshDatabaseAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	for range 2 {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		migrationTestCount(t, st.db, "SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0 AND is_applied = 1", 2)
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationBaselineRollbackAndRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openUnversionedStore(t, path)
	execMigrationTest(t, legacy.db, "DROP TABLE folder_preferences;"+
		"ALTER TABLE settings RENAME COLUMN value TO old_value;"+
		"INSERT INTO settings (key, old_value, updated_at) VALUES ('upgrade.test', 'legacy', 1);")

	st, err := Open(path)
	if err == nil {
		st.Close()
		t.Fatal("opened an incompatible database")
	}
	if !strings.Contains(err.Error(), "settings.value") {
		t.Fatalf("migration failure should identify the missing column: %v", err)
	}
	migrationTestCount(t, legacy.db, "SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0", 0)
	migrationTestCount(t, legacy.db, "SELECT COUNT(*) FROM sqlite_schema WHERE name = 'folder_preferences'", 0)
	migrationTestCount(t, legacy.db, "SELECT COUNT(*) FROM settings WHERE old_value = 'legacy'", 1)

	execMigrationTest(t, legacy.db, "ALTER TABLE settings RENAME COLUMN old_value TO value")
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	migrationTestCount(t, st.db, "SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0 AND is_applied = 1", 2)
	migrationTestCount(t, st.db, "SELECT COUNT(*) FROM sqlite_schema WHERE name = 'folder_preferences'", 1)
	migrationTestCount(t, st.db, "SELECT COUNT(*) FROM settings WHERE value = 'legacy'", 1)
}

func TestOpenRejectsNewerSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	execMigrationTest(t, st.db, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (1000, 1)")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err == nil {
		st.Close()
		t.Fatal("opened a newer database")
	}
	if !strings.Contains(err.Error(), "1000") {
		t.Fatalf("expected the newer schema version in the error: %v", err)
	}
}

func TestConcurrentOpenAppliesEachMigrationOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy := openUnversionedStore(t, path)
	execMigrationTest(t, legacy.db, "DROP INDEX idx_folder_items_item_id_state")
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	const openers = 8
	start := make(chan struct{})
	results := make(chan error, openers)
	for range openers {
		go func() {
			<-start
			st, err := Open(path)
			if err == nil {
				err = st.Close()
			}
			results <- err
		}()
	}
	close(start)
	for range openers {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	migrationTestCount(t, st.db, "SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1 AND is_applied = 1", 1)
	migrationTestCount(t, st.db, "SELECT COUNT(*) FROM goose_db_version WHERE version_id = 2 AND is_applied = 1", 1)
}
