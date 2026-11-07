package main

import (
	"context"
	"errors"
	"magpiesync/internal/store"
	"path/filepath"
	"testing"
)

func TestScheduledSyncOnlyActiveAccounts(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i, uid := range []string{"one", "two", "three"} {
		id, err := st.UpsertAccount(ctx, "synthetic", uid, uid, "", []byte("test"), 100)
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if err := st.SetAccountState(ctx, id, "needs_reauth", 100); err != nil {
				t.Fatal(err)
			}
		}
	}
	var ids []int64
	syncActiveAccounts(st, func(id int64, trigger, scope string) (int64, error) {
		ids = append(ids, id)
		if trigger != "scheduled" || scope != "all" {
			t.Errorf("wrong invocation %q %q", trigger, scope)
		}
		return 0, errors.New("already running")
	})
	if len(ids) != 2 || ids[0] == 2 || ids[1] == 2 {
		t.Fatalf("called accounts %v", ids)
	}
}
