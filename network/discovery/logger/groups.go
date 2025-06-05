package compatible_logger

import (
	"log/slog"
	"slices"
)

func AppendAttrsToGroup(groups []string, actualAttrs []slog.Attr, newAttrs ...slog.Attr) []slog.Attr {
	actualAttrs = slices.Clone(actualAttrs)

	if len(groups) == 0 {
		return UniqAttrs(append(actualAttrs, newAttrs...))
	}

	for i := range actualAttrs {
		attr := actualAttrs[i]
		if attr.Key == groups[0] && attr.Value.Kind() == slog.KindGroup {
			actualAttrs[i] = slog.Group(groups[0], ToAnySlice(AppendAttrsToGroup(groups[1:], attr.Value.Group(), newAttrs...))...)
			return actualAttrs
		}
	}

	return UniqAttrs(
		append(
			actualAttrs,
			slog.Group(
				groups[0],
				ToAnySlice(AppendAttrsToGroup(groups[1:], []slog.Attr{}, newAttrs...))...,
			),
		),
	)
}

func UniqAttrs(attrs []slog.Attr) []slog.Attr {
	return uniqByLast(attrs, func(item slog.Attr) string {
		return item.Key
	})
}

func uniqByLast[T any, U comparable](collection []T, iteratee func(item T) U) []T {
	result := make([]T, 0, len(collection))
	seen := make(map[U]int, len(collection))
	seenIndex := 0

	for _, item := range collection {
		key := iteratee(item)

		if index, ok := seen[key]; ok {
			result[index] = item
			continue
		}

		seen[key] = seenIndex
		seenIndex++
		result = append(result, item)
	}

	return result
}

func ToAnySlice[T any](collection []T) []any {
	result := make([]any, len(collection))
	for i, item := range collection {
		result[i] = item
	}
	return result
}
