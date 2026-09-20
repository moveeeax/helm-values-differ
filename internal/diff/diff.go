package diff

import (
	"fmt"
	"sort"
)

// ChangeKind classifies an object-level or field-level change.
type ChangeKind string

const (
	Added   ChangeKind = "added"
	Removed ChangeKind = "removed"
	Changed ChangeKind = "changed"
)

// FieldChange is a single leaf-level difference within an object, addressed by
// a dotted path (list elements use [i]).
type FieldChange struct {
	Path string      `json:"path"`
	Kind ChangeKind  `json:"kind"`
	From interface{} `json:"from,omitempty"`
	To   interface{} `json:"to,omitempty"`
}

// ObjectDiff is the set of changes for one Kubernetes object.
type ObjectDiff struct {
	Object string        `json:"object"`
	Kind   ChangeKind    `json:"kind"`
	Fields []FieldChange `json:"fields,omitempty"`
}

// Result is the full diff between a base and a candidate render.
type Result struct {
	Diffs []ObjectDiff `json:"diffs"`
}

// Empty reports whether the two renders are semantically identical.
func (r *Result) Empty() bool { return len(r.Diffs) == 0 }

// Objects compares two sets of parsed objects by identity and returns the
// semantic diff. Objects present only in candidate are Added, only in base are
// Removed, and matched objects with differing fields are Changed with a list of
// field changes. Identical objects produce no entry.
func Objects(base, candidate []Object) *Result {
	baseIdx, _ := index(base)
	candIdx, candKeys := index(candidate)

	res := &Result{}
	// Added / changed, walked in candidate order for stable output.
	for _, id := range candKeys {
		cand := candIdx[id]
		b, ok := baseIdx[id]
		if !ok {
			res.Diffs = append(res.Diffs, ObjectDiff{Object: cand.String(), Kind: Added})
			continue
		}
		fields := diffValue("", b.Data, cand.Data)
		if len(fields) > 0 {
			sortFields(fields)
			res.Diffs = append(res.Diffs, ObjectDiff{Object: cand.String(), Kind: Changed, Fields: fields})
		}
	}
	// Removed: in base, not in candidate.
	var removed []string
	for id := range baseIdx {
		if _, ok := candIdx[id]; !ok {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		res.Diffs = append(res.Diffs, ObjectDiff{Object: baseIdx[id].String(), Kind: Removed})
	}
	return res
}

// diffValue recursively compares two decoded YAML values and returns the leaf
// changes under path.
func diffValue(path string, a, b interface{}) []FieldChange {
	// Both maps: recurse per key union.
	am, aok := a.(map[string]interface{})
	bm, bok := b.(map[string]interface{})
	if aok && bok {
		return diffMap(path, am, bm)
	}
	// Both slices: compare element-wise (positional).
	as, aok2 := a.([]interface{})
	bs, bok2 := b.([]interface{})
	if aok2 && bok2 {
		return diffSlice(path, as, bs)
	}
	// One side present, other absent (nil), or scalar mismatch.
	if !equal(a, b) {
		switch {
		case a == nil:
			return []FieldChange{{Path: path, Kind: Added, To: b}}
		case b == nil:
			return []FieldChange{{Path: path, Kind: Removed, From: a}}
		default:
			return []FieldChange{{Path: path, Kind: Changed, From: a, To: b}}
		}
	}
	return nil
}

func diffMap(path string, a, b map[string]interface{}) []FieldChange {
	keys := map[string]struct{}{}
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	var out []FieldChange
	for _, k := range sorted {
		out = append(out, diffValue(join(path, k), a[k], b[k])...)
	}
	return out
}

func diffSlice(path string, a, b []interface{}) []FieldChange {
	var out []FieldChange
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var av, bv interface{}
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		out = append(out, diffValue(fmt.Sprintf("%s[%d]", path, i), av, bv)...)
	}
	return out
}

// equal compares two decoded YAML values for semantic equality. Numbers are
// compared by normalized form so `2` and `2.0` from different renders don't
// register as a spurious change.
func equal(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if an, aok := toFloat(a); aok {
		if bn, bok := toFloat(b); bok {
			return an == bn
		}
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func join(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func sortFields(fs []FieldChange) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Path < fs[j].Path })
}

// SummaryCounts returns added/removed/changed object counts.
func (r *Result) SummaryCounts() (add, rem, chg int) {
	for _, d := range r.Diffs {
		switch d.Kind {
		case Added:
			add++
		case Removed:
			rem++
		case Changed:
			chg++
		}
	}
	return
}
