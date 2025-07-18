package compatible_logger

import (
	"log/slog"
	"reflect"
	"runtime"
	"slices"
)

func AppendRecordAttrsToAttrs(attrs []slog.Attr, groups []string, record *slog.Record) []slog.Attr {
	output := slices.Clone(attrs)

	groups = reverse(groups)
	record.Attrs(func(attr slog.Attr) bool {
		for i := range groups {
			attr = slog.Group(groups[i], attr)
		}
		output = append(output, attr)
		return true
	})

	return output
}

func reverse[T any](collection []T) []T {
	length := len(collection)
	half := length / 2

	for i := 0; i < half; i = i + 1 {
		j := length - 1 - i
		collection[i], collection[j] = collection[j], collection[i]
	}

	return collection
}

func ReplaceError(attrs []slog.Attr, errorKeys ...string) []slog.Attr {
	replaceAttr := func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) > 1 {
			return a
		}

		for i := range errorKeys {
			if a.Key == errorKeys[i] {
				if err, ok := a.Value.Any().(error); ok {
					return slog.Any(a.Key, FormatError(err))
				}
			}
		}
		return a
	}
	return ReplaceAttrs(replaceAttr, []string{}, attrs...)
}

func FormatError(err error) map[string]any {
	return map[string]any{
		"kind":  reflect.TypeOf(err).String(),
		"error": err.Error(),
		"stack": nil, // @TODO
	}
}

type ReplaceAttrFn = func(groups []string, a slog.Attr) slog.Attr

func ReplaceAttrs(fn ReplaceAttrFn, groups []string, attrs ...slog.Attr) []slog.Attr {
	for i := range attrs {
		attr := attrs[i]
		value := attr.Value.Resolve()
		if value.Kind() == slog.KindGroup {
			attrs[i].Value = slog.GroupValue(ReplaceAttrs(fn, append(groups, attr.Key), value.Group()...)...)
		} else if fn != nil {
			attrs[i] = fn(groups, attr)
		}
	}

	return attrs
}

func Source(sourceKey string, r *slog.Record) slog.Attr {
	fs := runtime.CallersFrames([]uintptr{r.PC})
	f, _ := fs.Next()
	var args []any
	if f.Function != "" {
		args = append(args, slog.String("function", f.Function))
	}
	if f.File != "" {
		args = append(args, slog.String("file", f.File))
	}
	if f.Line != 0 {
		args = append(args, slog.Int("line", f.Line))
	}

	return slog.Group(sourceKey, args...)
}

func AttrsToMap(attrs ...slog.Attr) map[string]any {
	output := map[string]any{}

	attrsByKey := groupValuesByKey(attrs)
	for k, values := range attrsByKey {
		v := mergeAttrValues(values...)
		if v.Kind() == slog.KindGroup {
			output[k] = AttrsToMap(v.Group()...)
		} else {
			output[k] = v.Any()
		}
	}

	return output
}

func groupValuesByKey(attrs []slog.Attr) map[string][]slog.Value {
	result := map[string][]slog.Value{}

	for _, item := range attrs {
		key := item.Key
		result[key] = append(result[key], item.Value)
	}

	return result
}

func mergeAttrValues(values ...slog.Value) slog.Value {
	v := values[0]

	for i := 1; i < len(values); i++ {
		if v.Kind() != slog.KindGroup || values[i].Kind() != slog.KindGroup {
			v = values[i]
			continue
		}

		v = slog.GroupValue(append(v.Group(), values[i].Group()...)...)
	}

	return v
}
