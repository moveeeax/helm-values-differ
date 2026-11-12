# helm-values-differ

[![ci](https://github.com/moveeeax/helm-values-differ/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/helm-values-differ/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Render a Helm chart with two different value sets and diff the **resulting Kubernetes manifests** — object by object, field by field — instead of the raw `values.yaml`.

A line diff of `values.yaml` is misleading: one flag can cascade into a dozen manifest changes, or into none. The only honest answer to "what does this values change actually do to the cluster?" is to render both and compare the output. That's what a reviewer would do by hand; this does it for them.

## What it does

```
helm-values-differ ./chart --base base.yaml --candidate candidate.yaml
```

It runs `helm template` on the chart twice (once per value set), parses both manifest streams, matches objects by identity (`kind` + `apiVersion` + `namespace` + `name`), and reports:

- objects **added** or **removed** between the two renders, and
- for matched objects, the exact **field paths** that changed, with `from → to` values.

```
$ helm-values-differ ./examples/demo-chart --base examples/base.yaml --candidate examples/candidate.yaml
~ Deployment default/release-name-web
    spec.replicas: 2 → 3

1 changed, 0 added, 0 removed
```

Change `replicaCount` from 2 to 3 and you get one line: the `spec.replicas` change. Nothing else, because nothing else moved.

## Why not just diff the YAML?

Because `values.yaml` and the rendered manifest are different things. A one-line values change can:

- touch many objects (a shared label, a global image registry),
- touch none (a value the templates don't reference on this path),
- or change something you didn't expect (a conditional block flipping on).

Diffing the render is the ground truth. This tool makes it a one-liner you can put in a PR.

## Install

```sh
go install github.com/moveeeax/helm-values-differ/cmd/helm-values-differ@latest
```

Requires the `helm` binary on `PATH` (v3 or v4). Point at a different binary with `--helm /path/to/helm`.

## Usage

```
usage: helm-values-differ CHART --base base.yaml --candidate candidate.yaml [--format text|markdown|json] [--show-secrets] [--mask REGEX]

  --base          value file for the BASE render (repeatable)
  --candidate     value file for the CANDIDATE render (repeatable)
  --format        text (default) | markdown | json
  --helm          path to the helm binary (default "helm")
  --show-secrets  print Secret data/stringData values instead of redacting them
  --mask          hide values of fields whose path or value matches REGEX (repeatable)
```

`--base` and `--candidate` are repeatable — pass several value files per side and they layer in order, exactly like `helm template -f a.yaml -f b.yaml`.

### Secrets and masking

Values under `data` and `stringData` of every `kind: Secret` are redacted by default in all formats. The output still names the keys that changed, so a reviewer sees what moved without the value:

```
$ helm-values-differ ./examples/demo-chart --base examples/base.yaml --candidate examples/candidate-secret.yaml
~ Secret default/release-name-db
    stringData.password: (changed)

1 changed, 0 added, 0 removed
```

Keys are reported as `(changed)`, `(added)` or `(removed)`. If a whole `data` or `stringData` block appears or disappears, each key in it gets its own line. Other Secret fields (`type`, labels, annotations) keep their values. The comparison runs on the real values before redaction, so redaction never hides or adds a change. In `--format json` a redacted field has `"redacted": true` and no `from`/`to`.

`--show-secrets` turns Secret redaction off, for local runs where the output does not leave your terminal.

`--mask REGEX` hides other values, in any object kind. A field is masked when the expression matches its path (`spec.template.spec.containers[0].env[2].value`) or the value on either side; both sides are hidden together so one cannot be read next to the other. Repeat the flag for several expressions. Syntax is Go [RE2](https://github.com/google/re2/wiki/Syntax). Masks also apply when `--show-secrets` is set.

```sh
helm-values-differ ./chart --base base.yaml --candidate candidate.yaml \
  --mask '\.env\[\d+\]\.value$' --mask '^AKIA'
```

### Markdown for PR comments

```sh
$ helm-values-differ ./chart --base base.yaml --candidate candidate.yaml --format markdown
### Manifest diff

1 changed · 0 added · 0 removed

- 🔧 **Deployment default/release-name-web**
  - `spec.replicas: 2 → 3`
```

### JSON for tooling

`--format json` emits the structured diff (`{"diffs":[{"object":...,"kind":...,"fields":[...]}]}`) for wiring into other automation.

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | No manifest changes |
| `1`  | Manifest changes found |
| `2`  | Runtime error (helm missing, bad chart, parse failure) |

Exit `1` on any change makes it usable as a "did this PR actually change the cluster?" gate.

## How it works

The diff engine (`internal/diff`) is deliberately render-agnostic and does not shell out — it takes rendered YAML bytes and produces the diff, so it's fully unit-tested offline. Numbers are compared numerically (`2` and `2.0` are equal, not a spurious change). Empty and comment-only documents in the helm output are skipped. Only the CLI layer invokes `helm template`.

## License

MIT — see [LICENSE](LICENSE).
