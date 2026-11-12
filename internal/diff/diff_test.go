package diff

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// fakeRenderer returns a canned manifest per value-file key, so the whole
// RenderAndDiff path is exercised without a helm binary.
type fakeRenderer struct {
	out map[string]string
}

func (f fakeRenderer) Render(chart string, valueFiles []string) ([]byte, error) {
	// key on the single value file each side passes in these tests
	return []byte(f.out[valueFiles[0]]), nil
}

func deployment(replicas int) string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: ` + itoa(replicas) + `
  template:
    spec:
      containers:
        - name: app
          image: nginx:1.25
`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// TestAcceptance is the Notion acceptance criterion, verbatim: changing
// replicaCount 2->3 shows a single Deployment.spec.replicas change, nothing else.
func TestAcceptance(t *testing.T) {
	r := fakeRenderer{out: map[string]string{
		"base.yaml":      deployment(2),
		"candidate.yaml": deployment(3),
	}}
	res, err := RenderAndDiff(r, "./chart", []string{"base.yaml"}, []string{"candidate.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Diffs) != 1 {
		t.Fatalf("want exactly 1 object diff, got %d: %+v", len(res.Diffs), res.Diffs)
	}
	d := res.Diffs[0]
	if d.Kind != Changed {
		t.Fatalf("want Changed, got %s", d.Kind)
	}
	if d.Object != "Deployment default/web" {
		t.Fatalf("unexpected object label %q", d.Object)
	}
	if len(d.Fields) != 1 {
		t.Fatalf("want exactly 1 field change, got %d: %+v", len(d.Fields), d.Fields)
	}
	f := d.Fields[0]
	if f.Path != "spec.replicas" {
		t.Fatalf("field path = %q, want spec.replicas", f.Path)
	}
	if f.Kind != Changed {
		t.Fatalf("field kind = %s, want changed", f.Kind)
	}
	// 2 -> 3, compared numerically
	if from, _ := toFloat(f.From); from != 2 {
		t.Fatalf("from = %v, want 2", f.From)
	}
	if to, _ := toFloat(f.To); to != 3 {
		t.Fatalf("to = %v, want 3", f.To)
	}
}

func TestIdenticalRendersAreEmpty(t *testing.T) {
	r := fakeRenderer{out: map[string]string{
		"a.yaml": deployment(2),
		"b.yaml": deployment(2),
	}}
	res, _ := RenderAndDiff(r, "c", []string{"a.yaml"}, []string{"b.yaml"})
	if !res.Empty() {
		t.Fatalf("identical renders should diff empty, got %+v", res.Diffs)
	}
}

func TestNumericNoiseIgnored(t *testing.T) {
	// `2` vs `2.0` must not register as a change.
	base := "kind: ConfigMap\nmetadata:\n  name: c\ndata:\n  n: 2\n"
	cand := "kind: ConfigMap\nmetadata:\n  name: c\ndata:\n  n: 2.0\n"
	b, _ := Parse([]byte(base))
	c, _ := Parse([]byte(cand))
	res := Objects(b, c)
	if !res.Empty() {
		t.Fatalf("2 vs 2.0 should be equal, got %+v", res.Diffs)
	}
}

func TestAddedAndRemovedObjects(t *testing.T) {
	base := "kind: Service\nmetadata:\n  name: svc\n  namespace: default\n"
	cand := "kind: Ingress\nmetadata:\n  name: ing\n  namespace: default\n"
	b, _ := Parse([]byte(base))
	c, _ := Parse([]byte(cand))
	res := Objects(b, c)
	add, rem, chg := res.SummaryCounts()
	if add != 1 || rem != 1 || chg != 0 {
		t.Fatalf("want 1 added, 1 removed, 0 changed; got %d/%d/%d: %+v", add, rem, chg, res.Diffs)
	}
}

func TestNestedFieldChange(t *testing.T) {
	base := "kind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n        - name: app\n          image: nginx:1.25\n"
	cand := "kind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n        - name: app\n          image: nginx:1.27\n"
	b, _ := Parse([]byte(base))
	c, _ := Parse([]byte(cand))
	res := Objects(b, c)
	if len(res.Diffs) != 1 || len(res.Diffs[0].Fields) != 1 {
		t.Fatalf("want one nested field change, got %+v", res.Diffs)
	}
	got := res.Diffs[0].Fields[0].Path
	want := "spec.template.spec.containers[0].image"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestMultiDocParse(t *testing.T) {
	manifest := `# Source: chart/templates/a.yaml
apiVersion: v1
kind: Service
metadata:
  name: svc
---
# Source: chart/templates/b.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
---
`
	objs, err := Parse([]byte(manifest))
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("want 2 objects, got %d: %+v", len(objs), objs)
	}
}

func TestEmptyAndCommentDocsSkipped(t *testing.T) {
	objs, err := Parse([]byte("---\n# just a comment\n---\n\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 0 {
		t.Fatalf("want 0 objects, got %+v", objs)
	}
}

func TestSameNameDifferentKindAreDistinct(t *testing.T) {
	// A Service and Deployment both named "web" must not be matched together.
	base := "kind: Service\nmetadata:\n  name: web\n---\nkind: Deployment\nmetadata:\n  name: web\n"
	b, _ := Parse([]byte(base))
	res := Objects(b, b)
	if !res.Empty() {
		t.Fatalf("identical set should be empty, got %+v", res.Diffs)
	}
}

func TestMarkdownAndText(t *testing.T) {
	r := fakeRenderer{out: map[string]string{
		"base.yaml": deployment(2), "candidate.yaml": deployment(3),
	}}
	res, _ := RenderAndDiff(r, "c", []string{"base.yaml"}, []string{"candidate.yaml"})

	var md bytes.Buffer
	if err := res.WriteMarkdown(&md); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "spec.replicas") || !strings.Contains(md.String(), "Manifest diff") {
		t.Fatalf("markdown missing content:\n%s", md.String())
	}

	var txt bytes.Buffer
	if err := res.WriteText(&txt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt.String(), "spec.replicas: 2 → 3") {
		t.Fatalf("text missing change line:\n%s", txt.String())
	}
}

func TestJSONRoundTrip(t *testing.T) {
	r := fakeRenderer{out: map[string]string{
		"base.yaml": deployment(2), "candidate.yaml": deployment(3),
	}}
	res, _ := RenderAndDiff(r, "c", []string{"base.yaml"}, []string{"candidate.yaml"})
	var buf bytes.Buffer
	if err := res.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var back Result
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(back.Diffs) != 1 {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestMalformedYAMLErrors(t *testing.T) {
	_, err := Parse([]byte("kind: [unterminated\n"))
	if err == nil {
		t.Fatal("expected a parse error for malformed YAML")
	}
}

func secretDiff(t *testing.T, base, cand string) *Result {
	t.Helper()
	b, err := Parse([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse([]byte(cand))
	if err != nil {
		t.Fatal(err)
	}
	return Objects(b, c)
}

const secretHead = "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db\n  namespace: default\n"

func TestSecretValuesRedactedByDefault(t *testing.T) {
	base := secretHead + "data:\n  password: YWJj\n  user: YWRtaW4=\nstringData:\n  token: old-token\n"
	cand := secretHead + "data:\n  password: eHl6\n  user: YWRtaW4=\n  extra: bmV3\nstringData:\n  token: new-token\n"
	res := secretDiff(t, base, cand)
	res.Redact(RedactOptions{})

	var txt, md, js bytes.Buffer
	if err := res.WriteText(&txt); err != nil {
		t.Fatal(err)
	}
	if err := res.WriteMarkdown(&md); err != nil {
		t.Fatal(err)
	}
	if err := res.WriteJSON(&js); err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"text": txt.String(), "markdown": md.String(), "json": js.String()} {
		for _, secret := range []string{"YWJj", "eHl6", "bmV3", "old-token", "new-token"} {
			if strings.Contains(out, secret) {
				t.Fatalf("%s output leaks %q:\n%s", name, secret, out)
			}
		}
	}
	for _, want := range []string{"data.extra: (added)", "data.password: (changed)", "stringData.token: (changed)"} {
		if !strings.Contains(txt.String(), want) {
			t.Fatalf("text missing %q:\n%s", want, txt.String())
		}
	}
	if strings.Contains(txt.String(), "data.user") {
		t.Fatalf("unchanged key reported:\n%s", txt.String())
	}
	if !strings.Contains(js.String(), `"redacted": true`) {
		t.Fatalf("json missing redacted marker:\n%s", js.String())
	}
}

func TestSecretWholeDataMapExpandsToKeys(t *testing.T) {
	base := secretHead
	cand := secretHead + "stringData:\n  b: two\n  a: one\n"
	res := secretDiff(t, base, cand)
	res.Redact(RedactOptions{})
	f := res.Diffs[0].Fields
	if len(f) != 2 || f[0].Path != "stringData.a" || f[1].Path != "stringData.b" {
		t.Fatalf("want per-key entries stringData.a, stringData.b; got %+v", f)
	}
	for _, fc := range f {
		if !fc.Redacted || fc.Kind != Added || fc.To != nil {
			t.Fatalf("want redacted added entry without value, got %+v", fc)
		}
	}

	// And the reverse: the whole map removed.
	res = secretDiff(t, cand, base)
	res.Redact(RedactOptions{})
	var txt bytes.Buffer
	_ = res.WriteText(&txt)
	if !strings.Contains(txt.String(), "stringData.a: (removed)") || strings.Contains(txt.String(), "one") {
		t.Fatalf("removed map not redacted per key:\n%s", txt.String())
	}
}

func TestSecretMetadataNotRedacted(t *testing.T) {
	base := secretHead + "type: Opaque\ndata:\n  k: YQ==\n"
	cand := strings.Replace(secretHead, "name: db", "name: db\n  labels:\n    tier: backend", 1) + "type: Opaque\ndata:\n  k: YQ==\n"
	res := secretDiff(t, base, cand)
	res.Redact(RedactOptions{})
	var txt bytes.Buffer
	_ = res.WriteText(&txt)
	if !strings.Contains(txt.String(), "metadata.labels: <none> → map[tier:backend]") {
		t.Fatalf("non-data Secret field should keep its value:\n%s", txt.String())
	}
}

func TestShowSecretsDisablesRedaction(t *testing.T) {
	base := secretHead + "data:\n  password: YWJj\n"
	cand := secretHead + "data:\n  password: eHl6\n"
	res := secretDiff(t, base, cand)
	res.Redact(RedactOptions{ShowSecrets: true})
	var txt bytes.Buffer
	_ = res.WriteText(&txt)
	if !strings.Contains(txt.String(), "data.password: YWJj → eHl6") {
		t.Fatalf("--show-secrets should print values:\n%s", txt.String())
	}
}

func TestConfigMapDataNotRedacted(t *testing.T) {
	base := "kind: ConfigMap\nmetadata:\n  name: c\ndata:\n  mode: a\n"
	cand := "kind: ConfigMap\nmetadata:\n  name: c\ndata:\n  mode: b\n"
	res := secretDiff(t, base, cand)
	res.Redact(RedactOptions{})
	if f := res.Diffs[0].Fields[0]; f.Redacted || f.To != "b" {
		t.Fatalf("ConfigMap data must not be redacted, got %+v", f)
	}
}

func TestMaskByPathAndValue(t *testing.T) {
	base := "kind: Deployment\nmetadata:\n  name: web\nspec:\n  apiKey: AKIAOLD\n  token: t1\n  replicas: 2\n"
	cand := "kind: Deployment\nmetadata:\n  name: web\nspec:\n  apiKey: plain\n  token: t2\n  replicas: 3\n"
	res := secretDiff(t, base, cand)
	res.Redact(RedactOptions{Masks: []*regexp.Regexp{
		regexp.MustCompile(`\.token$`), // by path
		regexp.MustCompile(`^AKIA`),    // by value: matches the base side only
	}})
	var txt bytes.Buffer
	_ = res.WriteText(&txt)
	out := txt.String()
	for _, leak := range []string{"AKIAOLD", "plain", "t1", "t2"} {
		if strings.Contains(out, leak) {
			t.Fatalf("mask leaked %q:\n%s", leak, out)
		}
	}
	for _, want := range []string{"spec.apiKey: (changed)", "spec.token: (changed)", "spec.replicas: 2 → 3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestMaskAppliesWithShowSecrets(t *testing.T) {
	base := secretHead + "data:\n  password: YWJj\n  user: YQ==\n"
	cand := secretHead + "data:\n  password: eHl6\n  user: Yg==\n"
	res := secretDiff(t, base, cand)
	res.Redact(RedactOptions{ShowSecrets: true, Masks: []*regexp.Regexp{regexp.MustCompile(`password`)}})
	var txt bytes.Buffer
	_ = res.WriteText(&txt)
	if !strings.Contains(txt.String(), "data.password: (changed)") || !strings.Contains(txt.String(), "data.user: YQ== → Yg==") {
		t.Fatalf("unexpected output:\n%s", txt.String())
	}
}
