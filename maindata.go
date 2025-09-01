package qbittorrent

import (
	"context"

	"golang.org/x/exp/slices"
)

func (dest *MainData) Update(ctx context.Context, c *Client) error {
	source, err := c.SyncMainDataCtx(ctx, int64(dest.Rid))
	if err != nil {
		return err
	}

	if source.FullUpdate {
		*dest = *source
		return nil
	}

	dest.Rid = source.Rid
	dest.ServerState = source.ServerState

	// Update cache metadata if present
	if source.CacheMetadata != nil {
		dest.CacheMetadata = &CacheMetadata{
			Source:      source.CacheMetadata.Source,
			Age:         source.CacheMetadata.Age,
			IsStale:     source.CacheMetadata.IsStale,
			NextRefresh: source.CacheMetadata.NextRefresh,
			HasMore:     source.CacheMetadata.HasMore,
		}
	}

	merge(source.Categories, &dest.Categories)
	merge(source.Torrents, &dest.Torrents)
	merge(source.Trackers, &dest.Trackers)
	remove(source.CategoriesRemoved, &dest.Categories)
	remove(source.TorrentsRemoved, &dest.Torrents)
	mergeSlice(source.Tags, &dest.Tags)
	removeSlice(source.TagsRemoved, &dest.Tags)
	return nil
}

func merge[T map[string]V, V any](s T, d *T) {
	for k, v := range s {
		(*d)[k] = v
	}
}

func remove[T map[string]V, V any](s []string, d *T) {
	for _, v := range s {
		delete(*d, v)
	}
}

func mergeSlice[T []string](s T, d *T) {
	*d = append(*d, s...)
	slices.Sort(*d)
	*d = slices.Compact(*d)
}

func removeSlice[T []string](s T, d *T) {
	for i := 0; i < len(*d); i++ {
		if k := (*d)[i]; len(k) != 0 {
			match := false
			for _, c := range s {
				if c == k {
					match = true
					break
				}
			}

			if !match {
				continue
			}
		}

		(*d)[i] = (*d)[len(*d)-1]
		(*d) = (*d)[:len(*d)-1]
		i--
	}
}
