package store

import "context"

// SetSettingIfAbsent inserts a setting without replacing an existing value.
// It reports whether this call inserted the setting.
func (s *Store) SetSettingIfAbsent(ctx context.Context, key, value string, now int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(key) DO NOTHING`, key, value, now)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows == 1, err
}
