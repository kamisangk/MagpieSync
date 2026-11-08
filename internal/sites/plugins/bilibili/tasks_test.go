//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"magpiesync/internal/media"
	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

type taskVideoClient struct {
	sites.VideoClient
	requested string
	failID    string
}

func (c *taskVideoClient) VideoDetail(_ context.Context, id string) (*media.VideoDetail, error) {
	c.requested = id
	if id == c.failID {
		return nil, errors.New("video detail unavailable")
	}
	return &media.VideoDetail{RemoteID: "BV11pa26dEit", Title: "test", Pages: []media.VideoPage{{PartID: 123, Page: 1, Title: "part"}}}, nil
}
func (c *taskVideoClient) VideoTags(context.Context, string) ([]string, error) { return nil, nil }
func TestBuildTasksPreservesSyncedItemIdentity(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c := &taskVideoClient{}
	const aid = "117370534364338"
	inputs, err := buildTasks(context.Background(), c, sites.AccountContext{Store: st, Account: &store.Account{ID: 1}}, sites.TaskRequest{Items: []store.Item{{RemoteID: aid}}, SourceTitle: "TEST"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 {
		t.Fatalf("tasks=%v", inputs)
	}
	if c.requested != aid || inputs[0].RemoteID != aid {
		t.Fatalf("changed identity: %q %+v", c.requested, inputs[0])
	}
	if inputs[0].PageURL != "https://www.bilibili.com/video/BV11pa26dEit" || inputs[0].PartID != 123 {
		t.Fatalf("wrong resolved metadata: %+v", inputs[0])
	}
}

func TestBuildTasksRejectsPartialResults(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	c := &taskVideoClient{failID: "unavailable"}
	inputs, err := buildTasks(context.Background(), c, sites.AccountContext{Store: st, Account: &store.Account{ID: 1}}, sites.TaskRequest{
		Items: []store.Item{{RemoteID: "available"}, {RemoteID: "unavailable"}},
	})
	if err == nil {
		t.Fatal("expected detail failure")
	}
	if len(inputs) != 0 {
		t.Fatalf("partial tasks returned with error: %+v", inputs)
	}
}
