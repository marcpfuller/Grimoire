package db

import "database/sql"

// UserVersionForTest reads PRAGMA user_version from the underlying SQLite
// connection. Exported for use by the external db_test package only.
func UserVersionForTest(s Store) (int, error) {
	ss := s.(*sqliteStore) //nolint:forcetypeassert // only called in tests with known type
	var ver int
	err := ss.db.QueryRow(`PRAGMA user_version`).Scan(&ver)
	return ver, err
}

// MigrationsForTest returns a copy of the migrations table so tests can
// assert the expected latest version without hardcoding it.
func MigrationsForTest() []struct{ Version int } {
	out := make([]struct{ Version int }, len(migrations))
	for i, m := range migrations {
		out[i].Version = m.version
	}
	return out
}

// LatestVersionForTest returns the version number of the last registered migration.
func LatestVersionForTest() int {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].version
}

// OpenRawForTest opens the database and returns the underlying *sql.DB so
// tests can manipulate user_version directly.
func OpenRawForTest(path string) (*sql.DB, error) {
	s, err := Open(path)
	if err != nil {
		return nil, err
	}
	return s.(*sqliteStore).db, nil
}
