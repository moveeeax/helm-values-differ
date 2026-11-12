package diff

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RedactOptions controls which field values are hidden before the diff is
// written. The zero value redacts Secret data and masks nothing else.
type RedactOptions struct {
	// ShowSecrets disables the default redaction of Secret data/stringData.
	ShowSecrets bool
	// Masks hides the values of any field whose path, or the string form of
	// either of its values, matches one of these expressions. Applies to every
	// object kind and is not affected by ShowSecrets.
	Masks []*regexp.Regexp
}

// Redact hides field values in place according to opts. A redacted field keeps
// its path and change kind (so the reader still sees which keys moved), drops
// From/To and sets Redacted. Comparison already happened on the real values,
// so redaction never hides or invents a change.
func (r *Result) Redact(opts RedactOptions) {
	for i := range r.Diffs {
		d := &r.Diffs[i]
		if len(d.Fields) == 0 {
			continue
		}
		secret := d.resource == "Secret" && !opts.ShowSecrets
		out := make([]FieldChange, 0, len(d.Fields))
		for _, f := range d.Fields {
			if secret && isSecretDataPath(f.Path) {
				out = append(out, redactSecretField(f)...)
				continue
			}
			if matchesMask(opts.Masks, f) {
				out = append(out, redacted(f))
				continue
			}
			out = append(out, f)
		}
		sortFields(out)
		d.Fields = out
	}
}

// isSecretDataPath reports whether path is a Secret's data or stringData map,
// or anything under it.
func isSecretDataPath(path string) bool {
	for _, root := range []string{"data", "stringData"} {
		if path == root || strings.HasPrefix(path, root+".") {
			return true
		}
	}
	return false
}

// redactSecretField redacts one Secret data field. When the whole data or
// stringData map appeared or disappeared, it is expanded into one entry per
// key, so the output still names the keys without their values.
func redactSecretField(f FieldChange) []FieldChange {
	var whole interface{}
	switch f.Kind {
	case Added:
		whole = f.To
	case Removed:
		whole = f.From
	}
	m, ok := whole.(map[string]interface{})
	if !ok || strings.Contains(f.Path, ".") {
		return []FieldChange{redacted(f)}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]FieldChange, 0, len(keys))
	for _, k := range keys {
		out = append(out, FieldChange{Path: join(f.Path, k), Kind: f.Kind, Redacted: true})
	}
	if len(out) == 0 {
		// An empty map carries no values; report it as is.
		return []FieldChange{f}
	}
	return out
}

func matchesMask(masks []*regexp.Regexp, f FieldChange) bool {
	for _, re := range masks {
		if re.MatchString(f.Path) {
			return true
		}
		// Check both sides: masking only the matching side would still leak
		// the other one next to it.
		if f.From != nil && re.MatchString(fmt.Sprintf("%v", f.From)) {
			return true
		}
		if f.To != nil && re.MatchString(fmt.Sprintf("%v", f.To)) {
			return true
		}
	}
	return false
}

func redacted(f FieldChange) FieldChange {
	return FieldChange{Path: f.Path, Kind: f.Kind, Redacted: true}
}
