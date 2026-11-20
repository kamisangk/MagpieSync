package store

import (
	"path/filepath"
	"testing"
)

func TestSetSettingIfAbsentPreservesExistingValue(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	const key = "test.setting"
	if inserted, err := st.SetSettingIfAbsent(t.Context(), key, "first", 1); err != nil || !inserted {
		t.Fatalf("first insert = %v, %v", inserted, err)
	}
	if inserted, err := st.SetSettingIfAbsent(t.Context(), key, "second", 2); err != nil || inserted {
		t.Fatalf("repeat insert = %v, %v", inserted, err)
	}
	if value, found, err := st.GetSetting(t.Context(), key); err != nil || !found || value != "first" {
		t.Fatalf("stored setting = %q, %v, %v", value, found, err)
	}
}
