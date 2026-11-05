//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bilibili

import (
	"context"

	"magpiesync/internal/sites"
	"magpiesync/internal/store"
)

// BuildTasks expands synced bilibili items into one task per video page. It
// lives here, not in the generic manager, because it needs the video API.
func (driver) BuildTasks(ctx context.Context, in sites.AccountContext, req sites.TaskRequest) ([]store.DownloadTaskInput, error) {
	video, err := (driver{}).NewVideoClient(ctx, in)
	if err != nil {
		return nil, err
	}
	inputs := make([]store.DownloadTaskInput, 0, len(req.Items))
	var lastErr error
	for _, item := range req.Items {
		bvid := item.RemoteID
		if bvid == "" {
			bvid = item.RemoteID
		}
		if bvid == "" {
			continue
		}
		detail, err := video.VideoDetail(ctx, bvid)
		if err != nil {
			lastErr = err
			continue
		}
		tags, _ := video.VideoTags(ctx, bvid)
		for _, p := range detail.Pages {
			inputs = append(inputs, store.DownloadTaskInput{
				Site:        "bilibili",
				PageURL:     "https://www.bilibili.com/video/" + detail.RemoteID,
				RemoteID:    detail.RemoteID,
				PartID:      p.PartID,
				VideoTitle:  detail.Title,
				PageTitle:   p.Title,
				PageNo:      p.Page,
				PageCount:   len(detail.Pages),
				Duration:    p.Duration,
				CoverURL:    detail.CoverURL,
				AuthorID:    detail.AuthorID,
				AuthorName:  detail.AuthorName,
				PubTime:     detail.PublishedAt,
				FavTime:     in.Store.FavoriteTime(ctx, in.Account.ID, bvid),
				SourceTitle: req.SourceTitle,
				Tags:        tags,
				Summary:     detail.Summary,
			})
		}
	}
	if len(inputs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return inputs, nil
}
