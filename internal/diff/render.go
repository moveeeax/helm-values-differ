package diff

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Renderer turns a chart + value files into a rendered manifest. The default
// implementation shells out to `helm template`; tests inject a fake.
type Renderer interface {
	Render(chart string, valueFiles []string) ([]byte, error)
}

// HelmCLI renders via the helm binary on PATH.
type HelmCLI struct {
	// Bin is the helm executable; defaults to "helm".
	Bin string
}

// Render runs `helm template <chart> -f v1 -f v2 ...`.
func (h HelmCLI) Render(chart string, valueFiles []string) ([]byte, error) {
	bin := h.Bin
	if bin == "" {
		bin = "helm"
	}
	args := []string{"template", chart}
	for _, f := range valueFiles {
		args = append(args, "-f", f)
	}
	cmd := exec.Command(bin, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("helm template failed: %v: %s", err, errBuf.String())
	}
	return out.Bytes(), nil
}

// RenderAndDiff renders base and candidate value sets against the same chart and
// returns the semantic manifest diff.
func RenderAndDiff(r Renderer, chart string, baseValues, candidateValues []string) (*Result, error) {
	baseManifest, err := r.Render(chart, baseValues)
	if err != nil {
		return nil, fmt.Errorf("rendering base: %w", err)
	}
	candManifest, err := r.Render(chart, candidateValues)
	if err != nil {
		return nil, fmt.Errorf("rendering candidate: %w", err)
	}
	baseObjs, err := Parse(baseManifest)
	if err != nil {
		return nil, fmt.Errorf("parsing base manifest: %w", err)
	}
	candObjs, err := Parse(candManifest)
	if err != nil {
		return nil, fmt.Errorf("parsing candidate manifest: %w", err)
	}
	return Objects(baseObjs, candObjs), nil
}
