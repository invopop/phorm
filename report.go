package phorm

import (
	"fmt"
	"strings"
)

// maxProblems keeps a report readable, the header still counts every problem.
const maxProblems = 25

// Report renders the findings into a single human readable block, ready to carry inside an error message.
func (r *ValidateXmlResponse) Report() string {
	if r == nil {
		return "validation failed: no validation result available"
	}

	errorCount, warningCount := r.count()
	total := errorCount + warningCount

	var b strings.Builder

	// Header: how badly it went, and what it was validated against.
	if r.Success {
		b.WriteString("validation passed: ")
	} else {
		b.WriteString("validation failed: ")
	}
	writeHeadline(&b, errorCount, warningCount)

	if r.ResolvedVesid != "" {
		b.WriteString(" against ")
		b.WriteString(r.ResolvedVesid)
	}
	if r.ErrorMessage != "" {
		b.WriteString("\nvalidator reported: ")
		b.WriteString(collapse(r.ErrorMessage))
	}
	if total == 0 {
		if !r.Success && r.ErrorMessage == "" {
			b.WriteString(", the validator rejected the document without detail")
		}
		return b.String()
	}

	shown := 0
	for _, layer := range r.Results {
		if layer == nil || len(layer.Errors)+len(layer.Warnings) == 0 || shown >= maxProblems {
			continue
		}

		writeLayer(&b, layer)

		// Errors before warnings: the errors are what blocks the document.
		for _, problem := range layer.Errors {
			if shown >= maxProblems {
				break
			}
			shown++
			writeProblem(&b, problem, shown, "ERROR")
		}
		for _, problem := range layer.Warnings {
			if shown >= maxProblems {
				break
			}
			shown++
			writeProblem(&b, problem, shown, "WARN")
		}
	}

	if remaining := total - shown; remaining > 0 {
		fmt.Fprintf(&b, "\n\n... and %s not shown", quantify(remaining, "more problem"))
	}

	return b.String()
}

// count totals the problems of every layer.
func (r *ValidateXmlResponse) count() (int, int) {
	errorCount, warningCount := 0, 0

	for _, layer := range r.Results {
		if layer == nil {
			continue
		}
		errorCount += len(layer.Errors)
		warningCount += len(layer.Warnings)
	}

	return errorCount, warningCount
}

// writeHeadline states how many problems of each severity were found.
func writeHeadline(b *strings.Builder, errorCount int, warningCount int) {
	switch {
	case errorCount > 0 && warningCount > 0:
		b.WriteString(quantify(errorCount, "error"))
		b.WriteString(" and ")
		b.WriteString(quantify(warningCount, "warning"))
	case errorCount > 0:
		b.WriteString(quantify(errorCount, "error"))
	case warningCount > 0:
		b.WriteString(quantify(warningCount, "warning"))
	default:
		b.WriteString("no problems reported")
	}
}

// writeLayer opens a section, e.g. "xsd (UBL-Invoice-2.1.xsd):", keeping only the artifact's file name.
func writeLayer(b *strings.Builder, layer *ValidationLayerResult) {
	heading := collapse(layer.ValidationType)
	if heading == "" {
		heading = "validation"
	}

	b.WriteString("\n\n")
	b.WriteString(heading)

	artifact := collapse(layer.ArtifactId)
	if i := strings.LastIndexAny(artifact, `/\`); i >= 0 {
		artifact = artifact[i+1:]
	}
	if artifact != "" {
		b.WriteString(" (")
		b.WriteString(artifact)
		b.WriteString(")")
	}

	b.WriteString(":")
}

// writeProblem renders one finding, with where in the document it went wrong.
func writeProblem(b *strings.Builder, problem *ValidationError, position int, fallbackLevel string) {
	if problem == nil {
		return
	}

	level := strings.ToUpper(collapse(problem.Level))
	if level == "" {
		level = fallbackLevel
	}

	fmt.Fprintf(b, "\n  %d) %s", position, level)

	if problem.ErrorID != "" {
		b.WriteString(" [")
		b.WriteString(collapse(problem.ErrorID))
		b.WriteString("]")
	}
	if message := problemText(problem); message != "" {
		b.WriteString(" ")
		b.WriteString(message)
	} else {
		b.WriteString(" (no message provided)")
	}

	// Most specific location first.
	switch {
	case problem.Xpath != "" && problem.Location != "":
		b.WriteString("\n     at ")
		b.WriteString(collapse(problem.Xpath))
		b.WriteString(" (")
		b.WriteString(collapse(problem.Location))
		b.WriteString(")")
	case problem.Xpath != "":
		b.WriteString("\n     at ")
		b.WriteString(collapse(problem.Xpath))
	case problem.Location != "":
		b.WriteString("\n     at ")
		b.WriteString(collapse(problem.Location))
	}
}

// problemText drops the rule id many rule sets repeat in front of the message, as "[BR-01]-" or "BR-01:".
func problemText(problem *ValidationError) string {
	message := collapse(problem.Message)
	id := collapse(problem.ErrorID)
	if id == "" {
		return message
	}

	for _, prefix := range []string{"[" + id + "]", id} {
		rest, found := strings.CutPrefix(message, prefix)
		if !found {
			continue
		}

		// A bare id needs a separator after it, or "BR-01" would eat "BR-011".
		trimmed := strings.TrimLeft(rest, " -:")
		if trimmed == "" || (prefix == id && trimmed == rest) {
			continue
		}

		return trimmed
	}

	return message
}

// collapse flattens the multi-line, indented texts validators tend to carry into a single line.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// quantify writes "1 error" / "3 errors" and friends.
func quantify(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
