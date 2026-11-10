package download

import (
	"context"

	"magpiesync/internal/store"
)

func DeleteAccount(ctx context.Context, st *store.Store, accountID int64, deleteFiles bool) (store.AccountDeletionResult, error) {
	return deleteAccount(ctx, st, accountID, deleteFiles, false)
}

func (m *Manager) DeleteAccount(ctx context.Context, accountID int64, deleteFiles bool) (store.AccountDeletionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.cancels {
		task, err := m.store.GetDownloadTask(ctx, id)
		if err != nil {
			return store.AccountDeletionResult{}, err
		}
		if task.AccountID == accountID {
			return store.AccountDeletionResult{}, store.ErrAccountDeletionConflict
		}
	}
	return deleteAccount(ctx, m.store, accountID, deleteFiles, len(m.cancels) > 0)
}

func deleteAccount(ctx context.Context, st *store.Store, accountID int64, deleteFiles, workerActive bool) (store.AccountDeletionResult, error) {
	if !deleteFiles {
		return st.DeleteAccountWithCleanup(ctx, accountID, nil)
	}
	return st.DeleteAccountWithCleanup(ctx, accountID, func(snapshot store.FolderCleanupSnapshot) store.FolderDeletionResult {
		snapshot.RunningDownload = snapshot.RunningDownload || workerActive
		return cleanupFolderFiles(ctx, snapshot)
	})
}
