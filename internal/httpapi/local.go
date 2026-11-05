package httpapi

import (
	"context"

	"magpiesync/internal/store"
)

func (s *Server) attachItemLocal(ctx context.Context, accountID int64, items []store.Item) error {
	remoteIDs := make([]string, 0, len(items))
	for _, it := range items {
		if it.RemoteID != "" {
			remoteIDs = append(remoteIDs, it.RemoteID)
		}
	}
	statuses, err := s.store.LocalStatuses(ctx, accountID, remoteIDs)
	if err != nil {
		return err
	}
	for i := range items {
		if st, ok := statuses[items[i].RemoteID]; ok {
			items[i].Local = st
		} else {
			items[i].Local = store.LocalStatus{State: "none"}
		}
	}
	return nil
}

func (s *Server) attachFolderLocal(ctx context.Context, accountID int64, folders []store.Folder) error {
	counts, err := s.store.FolderLocalCounts(ctx, accountID)
	if err != nil {
		return err
	}
	for i := range folders {
		c := counts[folders[i].ID]
		folders[i].LocalTotal = c.Total
		folders[i].LocalDone = c.Done
	}
	return nil
}
