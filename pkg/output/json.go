package output

import (
	"encoding/json"
	"io"
	"os"

	"github.com/sozercan/unstuck/pkg/types"
)

// JSONPrinter outputs JSON formatted data
type JSONPrinter struct {
	out    io.Writer
	pretty bool
}

// NewJSONPrinter creates a new JSON printer
func NewJSONPrinter(pretty bool) *JSONPrinter {
	return &JSONPrinter{
		out:    os.Stdout,
		pretty: pretty,
	}
}

// SetOutput sets the output writer
func (p *JSONPrinter) SetOutput(out io.Writer) {
	p.out = out
}

// PrintDiagnosis outputs a diagnosis report as JSON
func (p *JSONPrinter) PrintDiagnosis(report *types.DiagnosisReport) error {
	encoder := json.NewEncoder(p.out)
	if p.pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(report)
}

// PrintPlan outputs a plan as JSON
func (p *JSONPrinter) PrintPlan(plan *types.Plan) error {
	encoder := json.NewEncoder(p.out)
	if p.pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(plan)
}

// PrintApplyResult outputs an apply result as JSON
func (p *JSONPrinter) PrintApplyResult(result *types.ApplyResult) error {
	encoder := json.NewEncoder(p.out)
	if p.pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(result)
}

// PrintAny outputs any value as JSON
func (p *JSONPrinter) PrintAny(v interface{}) error {
	encoder := json.NewEncoder(p.out)
	if p.pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(v)
}
