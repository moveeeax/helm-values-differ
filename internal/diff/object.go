// Package diff renders-agnostic: it parses Kubernetes manifests into objects
// keyed by identity and produces a semantic field-level diff between two sets.
// It never shells out — the caller supplies the rendered YAML — so the whole
// engine is testable offline.
package diff

import (
	"fmt"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Object is a single Kubernetes manifest and its identity.
type Object struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Data       map[string]interface{}
}

// ID is the stable identity used to match objects across the two renders.
// Two objects with the same ID in base and candidate are considered "the same
// object, possibly changed"; an ID present on only one side is added/removed.
func (o Object) ID() string {
	ns := o.Namespace
	if ns == "" {
		ns = "~" // default/cluster-scoped bucket, kept distinct from a real ""
	}
	return fmt.Sprintf("%s/%s/%s/%s", o.Kind, o.APIVersion, ns, o.Name)
}

// String is a short human label for the object.
func (o Object) String() string {
	if o.Namespace != "" {
		return fmt.Sprintf("%s %s/%s", o.Kind, o.Namespace, o.Name)
	}
	return fmt.Sprintf("%s %s", o.Kind, o.Name)
}

// Parse splits a multi-document YAML stream into objects, skipping empty
// documents and anything without a kind/name (helm often emits blank docs and
// comment-only separators). It errors only on genuinely malformed YAML.
func Parse(manifest []byte) ([]Object, error) {
	var objs []Object
	for i, doc := range splitDocs(manifest) {
		trimmed := strings.TrimSpace(doc)
		if trimmed == "" || onlyComments(trimmed) {
			continue
		}
		var m map[string]interface{}
		if err := yaml.Unmarshal([]byte(doc), &m); err != nil {
			return nil, fmt.Errorf("document %d: %w", i+1, err)
		}
		if len(m) == 0 {
			continue
		}
		kind, _ := m["kind"].(string)
		if kind == "" {
			continue // not a Kubernetes object (e.g. a values fragment)
		}
		o := Object{
			APIVersion: strval(m, "apiVersion"),
			Kind:       kind,
			Data:       m,
		}
		if md, ok := m["metadata"].(map[string]interface{}); ok {
			o.Name = strval(md, "name")
			o.Namespace = strval(md, "namespace")
		}
		objs = append(objs, o)
	}
	return objs, nil
}

// splitDocs separates a YAML stream on `---` document markers.
func splitDocs(b []byte) []string {
	lines := strings.Split(string(b), "\n")
	var docs []string
	var cur []string
	for _, ln := range lines {
		if strings.TrimRight(ln, " \t") == "---" {
			docs = append(docs, strings.Join(cur, "\n"))
			cur = nil
			continue
		}
		cur = append(cur, ln)
	}
	docs = append(docs, strings.Join(cur, "\n"))
	return docs
}

func onlyComments(s string) bool {
	for _, ln := range strings.Split(s, "\n") {
		t := strings.TrimSpace(ln)
		if t != "" && !strings.HasPrefix(t, "#") {
			return false
		}
	}
	return true
}

func strval(m map[string]interface{}, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// index maps object ID to object, and preserves a sorted key order.
func index(objs []Object) (map[string]Object, []string) {
	m := make(map[string]Object, len(objs))
	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		id := o.ID()
		m[id] = o
		keys = append(keys, id)
	}
	sort.Strings(keys)
	return m, keys
}
