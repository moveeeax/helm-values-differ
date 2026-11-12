package diff

import (
	"encoding/json"
	"fmt"
	"io"
)

// WriteText renders the diff as a human-readable report.
func (r *Result) WriteText(w io.Writer) error {
	if r.Empty() {
		_, err := fmt.Fprintln(w, "No changes — the two value sets render identical manifests.")
		return err
	}
	for _, d := range r.Diffs {
		switch d.Kind {
		case Added:
			if _, err := fmt.Fprintf(w, "+ %s (added)\n", d.Object); err != nil {
				return err
			}
		case Removed:
			if _, err := fmt.Fprintf(w, "- %s (removed)\n", d.Object); err != nil {
				return err
			}
		case Changed:
			if _, err := fmt.Fprintf(w, "~ %s\n", d.Object); err != nil {
				return err
			}
			for _, f := range d.Fields {
				if _, err := fmt.Fprintf(w, "    %s\n", fieldLine(f)); err != nil {
					return err
				}
			}
		}
	}
	add, rem, chg := r.SummaryCounts()
	_, err := fmt.Fprintf(w, "\n%d changed, %d added, %d removed\n", chg, add, rem)
	return err
}

// WriteMarkdown renders the diff for pasting into a PR comment.
func (r *Result) WriteMarkdown(w io.Writer) error {
	if r.Empty() {
		_, err := fmt.Fprintln(w, "**No manifest changes** — the two value sets render identically.")
		return err
	}
	add, rem, chg := r.SummaryCounts()
	if _, err := fmt.Fprintf(w, "### Manifest diff\n\n%d changed · %d added · %d removed\n\n", chg, add, rem); err != nil {
		return err
	}
	for _, d := range r.Diffs {
		switch d.Kind {
		case Added:
			if _, err := fmt.Fprintf(w, "- ➕ **%s** added\n", d.Object); err != nil {
				return err
			}
		case Removed:
			if _, err := fmt.Fprintf(w, "- ➖ **%s** removed\n", d.Object); err != nil {
				return err
			}
		case Changed:
			if _, err := fmt.Fprintf(w, "- 🔧 **%s**\n", d.Object); err != nil {
				return err
			}
			for _, f := range d.Fields {
				if _, err := fmt.Fprintf(w, "  - `%s`\n", fieldLine(f)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// WriteJSON renders the raw diff as indented JSON.
func (r *Result) WriteJSON(w io.Writer) error {
	if r.Diffs == nil {
		r.Diffs = []ObjectDiff{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func fieldLine(f FieldChange) string {
	if f.Redacted {
		return fmt.Sprintf("%s: (%s)", f.Path, f.Kind)
	}
	switch f.Kind {
	case Added:
		return fmt.Sprintf("%s: <none> → %v", f.Path, f.To)
	case Removed:
		return fmt.Sprintf("%s: %v → <none>", f.Path, f.From)
	default:
		return fmt.Sprintf("%s: %v → %v", f.Path, f.From, f.To)
	}
}
