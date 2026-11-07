package scheduler

import (
	"context"
	"magpiesync/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigure(t *testing.T) {
	s := New(func() {})
	defer s.Stop()
	for _, expr := range []string{"0 4 * * *", "0 */6 * * *", ""} {
		plan, err := Parse(expr)
		if err != nil {
			t.Fatal(err)
		}
		s.Configure(plan)
		want := 1
		if expr == "" {
			want = 0
		}
		if len(s.cron.Entries()) != want {
			t.Fatalf("entries for %q", expr)
		}
	}
	for _, expr := range []string{"99 4 * * *", "@daily", "CRON_TZ=UTC 0 4 * * *", "0 0 31 2 *"} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("accepted %q", expr)
		}
	}
	plan, _ := Parse("0 4 * * *")
	s.Configure(plan)
	if _, err := Parse("bad"); err == nil {
		t.Fatal("accepted invalid")
	}
	if len(s.cron.Entries()) != 1 {
		t.Fatal("invalid changed schedule")
	}
}

func TestLoadSeed(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := Load(ctx, st, "0 4 * * *")
	if err != nil || got != "0 4 * * *" {
		t.Fatalf("seed %q %v", got, err)
	}
	got, err = Load(ctx, st, "bad")
	if err != nil || got != "0 4 * * *" {
		t.Fatalf("stored wins %q %v", got, err)
	}
	if err := st.SetSettings(ctx, map[string]string{Key: ""}, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	got, err = Load(ctx, st, "0 4 * * *")
	if err != nil || got != "" {
		t.Fatalf("empty wins %q %v", got, err)
	}
}

func TestLoadDefaultAndInvalidLegacy(t *testing.T) {
	ctx := context.Background()
	for _, legacy := range []string{"", "bad"} {
		t.Run(legacy, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			value, err := Load(ctx, st, legacy)
			if legacy == "bad" {
				if err == nil {
					t.Fatal("accepted invalid seed")
				}
				if _, found, _ := st.GetSetting(ctx, Key); found {
					t.Fatal("persisted invalid seed")
				}
			} else {
				if err != nil || value != "" {
					t.Fatalf("default %q %v", value, err)
				}
				value, err = Load(ctx, st, "0 4 * * *")
				if err != nil || value != "" {
					t.Fatal("default not persisted")
				}
			}
		})
	}
}

func TestCronFiresAndStops(t *testing.T) {
	fired := make(chan struct{}, 4)
	s := New(func() { fired <- struct{}{} })
	defer s.Stop()
	plan, err := Parse("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	s.Configure(plan)
	select {
	case <-fired:
	case <-time.After(65 * time.Second):
		t.Fatal("cron did not fire at the next minute")
	}
	s.Configure(nil)
	if len(s.cron.Entries()) != 0 {
		t.Fatal("disabled schedule still registered")
	}
	select {
	case <-fired:
		t.Fatal("duplicate scheduled invocation")
	default:
	}
}
