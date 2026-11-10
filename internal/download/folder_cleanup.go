package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"magpiesync/internal/store"
)

func DeleteArchivedFolder(ctx context.Context, st *store.Store, accountID, folderID int64, deleteFiles bool, now int64) (store.FolderDeletionResult, error) {
	return deleteArchivedFolder(ctx, st, accountID, folderID, deleteFiles, false, now)
}

func (m *Manager) DeleteArchivedFolder(ctx context.Context, accountID, folderID int64, deleteFiles bool, now int64) (store.FolderDeletionResult, error) {
	// Lock order: manager worker registry, then database transaction. Workers do
	// not hold the registry mutex during database operations or filesystem work.
	m.mu.Lock()
	defer m.mu.Unlock()
	return deleteArchivedFolder(ctx, m.store, accountID, folderID, deleteFiles, len(m.cancels) > 0, now)
}

func deleteArchivedFolder(ctx context.Context, st *store.Store, accountID, folderID int64, deleteFiles, workerActive bool, now int64) (store.FolderDeletionResult, error) {
	if !deleteFiles {
		return st.DeleteArchivedFolder(ctx, accountID, folderID, nil, now)
	}
	return st.DeleteArchivedFolder(ctx, accountID, folderID, func(snapshot store.FolderCleanupSnapshot) store.FolderDeletionResult {
		snapshot.RunningDownload = snapshot.RunningDownload || workerActive
		return cleanupFolderFiles(ctx, snapshot)
	}, now)
}

func cleanupFolderFiles(ctx context.Context, snapshot store.FolderCleanupSnapshot) store.FolderDeletionResult {
	return cleanupFolderFilesUsing(ctx, snapshot, removeCleanupFile)
}

func cleanupFolderFilesUsing(ctx context.Context, snapshot store.FolderCleanupSnapshot, remove func(string, string) error) store.FolderDeletionResult {
	result := store.FolderDeletionResult{}
	references := snapshot.Tasks
	if snapshot.DeletingAccountID != 0 {
		references = nil
		for _, task := range snapshot.Tasks {
			if task.AccountID != snapshot.DeletingAccountID || task.Protected || !task.Candidate || task.State != "done" {
				references = append(references, task)
			}
		}
	}
	reasons := map[string]bool{}
	reason := func(value string) {
		if !reasons[value] {
			reasons[value] = true
			result.Reasons = append(result.Reasons, value)
		}
	}
	protected := map[string]bool{}
	genericSeen := map[string]bool{}
	for _, task := range snapshot.Tasks {
		if !task.Candidate {
			continue
		}
		key := task.Site + "\x00" + task.RemoteID
		if task.Protected {
			protected[key] = true
			reason("其他来源、账户或待完成任务仍引用的视频已保留")
			continue
		}
		if task.State != "done" || task.FilePath == "" {
			continue
		}
		if genericCleanupAsset(filepath.Base(task.FilePath)) {
			result.SkippedFiles++
			reason("目录级封面与元数据归属不明确，已保留")
			continue
		}
		if snapshot.RunningDownload {
			result.SkippedFiles++
			reason("有下载任务正在运行，已保留文件以避免冲突")
			continue
		}
		if ctx.Err() != nil {
			result.SkippedFiles++
			reason("操作已中断，未处理的文件已保留")
			continue
		}
		path, err := safeCleanupPath(snapshot.MediaDir, task.FilePath)
		if err != nil {
			result.SkippedFiles++
			reason("路径不安全或不在当前媒体目录内，文件已保留")
			continue
		}
		if cleanupPathShared(path, task.ID, references, false) {
			protected[key] = true
			reason("其他下载记录共用的文件已保留")
			continue
		}
		// Resolve aliases before unlinking: SameFile cannot detect a removed inode.
		companions, err := cleanupCompanions(path)
		if err != nil {
			result.SkippedFiles++
			reason("无法确认附属文件，已保留附属文件")
		}
		if err = remove(snapshot.MediaDir, path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result.FailedFiles++
			if snapshot.DeletingAccountID != 0 {
				reason("部分文件删除失败，文件仍保留在硬盘中")
			} else {
				reason("部分文件删除失败，原下载记录已保留")
			}
			continue
		}
		if err == nil {
			result.DeletedFiles++
		}
		result.MissingTaskIDs = append(result.MissingTaskIDs, task.ID)
		for _, companion := range companions {
			safe, checkErr := safeCleanupPath(snapshot.MediaDir, companion)
			if checkErr != nil || cleanupPathShared(safe, task.ID, references, true) {
				result.SkippedFiles++
				reason("共用或归属不明确的附属文件已保留")
				continue
			}
			if removeErr := remove(snapshot.MediaDir, safe); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				result.FailedFiles++
				reason("部分附属文件删除失败")
			} else if removeErr == nil {
				result.DeletedFiles++
			}
		}
		for _, name := range []string{"poster.jpg", "fanart.jpg", "tvshow.nfo", "person.nfo"} {
			asset := filepath.Join(filepath.Dir(path), name)
			if genericSeen[asset] {
				continue
			}
			genericSeen[asset] = true
			if _, err := os.Lstat(asset); err == nil {
				result.SkippedFiles++
				reason("目录级封面与元数据归属不明确，已保留")
			}
		}
	}
	result.ProtectedVideos = len(protected)
	return result
}

func pathKey(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	absolute = filepath.Clean(absolute)
	if runtime.GOOS == "windows" {
		absolute = strings.ToLower(absolute)
	}
	return absolute
}

func cleanupPathShared(path string, taskID int64, tasks []store.FolderCleanupTask, companion bool) bool {
	info, _ := os.Stat(path)
	for _, other := range tasks {
		if other.ID == taskID || other.FilePath == "" {
			continue
		}
		if pathKey(path) == pathKey(other.FilePath) {
			return true
		}
		if info != nil {
			if otherInfo, err := os.Stat(other.FilePath); err == nil && os.SameFile(info, otherInfo) {
				return true
			}
		}
		if companion {
			otherPath := pathKey(other.FilePath)
			base := strings.TrimSuffix(otherPath, filepath.Ext(otherPath))
			candidate := pathKey(path)
			if candidate == base+".nfo" || strings.HasPrefix(candidate, base+".") && (strings.HasSuffix(candidate, ".srt") || strings.HasSuffix(candidate, ".ass")) {
				return true
			}
		}
	}
	return false
}

func safeCleanupPath(root, path string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !withinCleanupRoot(root, path) {
		return "", errors.New("outside media directory")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	parent, err := cleanupResolvedParent(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(parent, filepath.Base(path))
	if !withinCleanupRoot(realRoot, resolved) {
		return "", errors.New("escaped media directory")
	}
	info, err := os.Lstat(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return resolved, nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	return resolved, nil
}

// Resolve the nearest existing ancestor so absent output directories still
// reconcile as missing, while an existing symlink cannot escape the root.
func cleanupResolvedParent(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = cleanupResolvedParent(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

// os.Root keeps the unlink contained even if a filesystem entry changes after
// validation; unlike a string-prefix check it cannot follow a swapped junction.
func removeCleanupFile(root, path string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	if !withinCleanupRoot(root, path) {
		return errors.New("outside media directory")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Remove(rel)
}

func withinCleanupRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func cleanupCompanions(path string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if genericCleanupAsset(name) {
			continue
		}
		if name == stem+".nfo" || strings.HasPrefix(name, stem+".") && (strings.HasSuffix(name, ".srt") || strings.HasSuffix(name, ".ass")) {
			paths = append(paths, filepath.Join(filepath.Dir(path), name))
		}
	}
	return paths, nil
}

func genericCleanupAsset(name string) bool {
	switch strings.ToLower(name) {
	case "poster.jpg", "fanart.jpg", "tvshow.nfo", "person.nfo":
		return true
	}
	return false
}
