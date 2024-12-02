package qbittorrent

import (
	"context"
	"reflect"

	"golang.org/x/exp/slices"
)

func (dest *MainData) Update(ctx context.Context, c *Client) error {
	source, err := c.SyncMainDataCtx(ctx, dest.Rid)
	if err != nil {
		return err
	}

	if source.FullUpdate {
		*dest = *source
		return nil
	}

	dest.Rid = source.Rid
	dest.ServerState = source.ServerState
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
	if s == nil {
		return
	}

	t := reflect.TypeFor[V]()
	for k, sv := range s {
		dv, ok := (*d)[k]
		if !ok {
			(*d)[k] = sv
			continue
		}

		dvp := reflect.ValueOf(&dv).Elem()
		svp := reflect.ValueOf(sv)
		for i := range t.NumField() {
			sp := svp.Field(i)
			if sp.IsNil() || !dvp.Field(i).CanSet() {
				continue
			}

			dvp.Field(i).Set(sp)
		}

		(*d)[k] = dv
	}
}

func remove[T map[string]V, V any](s []string, d *T) {
	if s == nil {
		return
	}

	for _, v := range s {
		delete(*d, v)
	}
}

func mergeSlice[T []string](s T, d *T) {
	if len(s) == 0 {
		return
	}

	*d = append(*d, s...)
	slices.Sort(*d)
	*d = slices.Compact(*d)
}

func removeSlice[T []string](s T, d *T) {
	if len(s) == 0 {
		return
	}

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
