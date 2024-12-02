package qbittorrent

import (
	"context"
	"reflect"

	"golang.org/x/exp/slices"
)

func InitializeMainData(ctx context.Context, c *Client) (*MainData, error) {
	m := MainData{
		client: c,
	}

	return &m, m.Update(ctx)
}

func (dest *MainData) Update(ctx context.Context) error {
	source, err := dest.client.SyncMainDataCtx(ctx, dest.Rid)
	if err != nil {
		return err
	}

	dest.Rid = source.Rid
	mergeStruct(source.ServerState, &dest.ServerState)
	merge(source.Categories, &dest.Categories)
	mergePtr(source.Torrents, &dest.Torrents)
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

	for k, sv := range s {
		(*d)[k] = sv
	}
}

func mergePtr[S map[string]SV, D map[string]DV, SV any, DV any](s S, d *D) {
	if s == nil {
		return
	}

	for k, sv := range s {
		dv, ok := (*d)[k]
		if !ok {
			dv = *new(DV)
		}

		mergeStruct(sv, &dv)
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

func mergeStruct[S any, D any](s S, d *D) {
	t := reflect.TypeFor[S]()

	dvp := reflect.ValueOf(d).Elem()
	svp := reflect.ValueOf(s)
	for i := range t.NumField() {
		sp := svp.Field(i)
		if sp.IsNil() || !dvp.Field(i).CanSet() {
			continue
		}

		dvp.Field(i).Set(sp.Elem())
	}
}
