package qbittorrent

import "golang.org/x/exp/slices"

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

