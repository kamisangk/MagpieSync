package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"magpiesync/internal/store"
)

func TestCompleteDownloadPersistsBeforeNotifying(t *testing.T) {
	for _, state := range []string{"running", "paused", "canceled"} {
		t.Run(state, func(t *testing.T) {
			ctx := t.Context()
			dir := t.TempDir()
			st, err := store.Open(filepath.Join(dir, "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			accountID, err := st.UpsertAccount(ctx, "fixture", "1", "tester", "", nil, 1)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := st.CreateDownloadTasks(ctx, accountID, 720, []store.DownloadTaskInput{{RemoteID: "one"}}, 1); err != nil {
				t.Fatal(err)
			}
			task, err := st.ClaimDownloadTask(ctx, accountID, 2)
			if err != nil || task == nil {
				t.Fatalf("claim task: %v, %v", task, err)
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := root.WriteFile("source.mp4", []byte("video"), 0o644); err != nil {
				t.Fatal(err)
			}
			output, err := publishDownload(ctx, root, filepath.Join(dir, "source.mp4"), filepath.Join(dir, "ready.mp4"), "one")
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "paused":
				err = st.PauseDownloadTask(ctx, task.ID, 3)
			case "canceled":
				err = st.CancelDownloadTask(ctx, task.ID, 3)
			}
			if err != nil {
				t.Fatal(err)
			}
			notifications := 0
			m := &Manager{store: st, onEvent: func(kind string, _ any) {
				notifications++
				got, err := st.GetDownloadTask(ctx, task.ID)
				if err != nil || got.State != "done" || got.FilePath != output.path || got.Progress != 1 || got.TotalBytes != output.size || got.Downloaded != output.size {
					t.Errorf("%s before durable completion: %+v, %v", kind, got, err)
				}
			}}
			cleaned := false
			finishCtx := ctx
			if state == "canceled" {
				var cancel context.CancelFunc
				finishCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			m.completeDownload(finishCtx, task.ID, output, 720, "mp4", "", func() { cleaned = true })
			got, err := st.GetDownloadTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, outputErr := root.Stat("ready.mp4")
			if state == "running" {
				if got.State != "done" || notifications == 0 || !cleaned || outputErr != nil {
					t.Fatalf("completion: %+v, notifications=%d cleaned=%t output=%v", got, notifications, cleaned, outputErr)
				}
			} else if got.State != state || notifications != 0 || cleaned != (state == "canceled") || !errors.Is(outputErr, os.ErrNotExist) {
				t.Fatalf("preserve %s: %+v, notifications=%d cleaned=%t output=%v", state, got, notifications, cleaned, outputErr)
			}
		})
	}
}
