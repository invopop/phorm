package phorm

import (
	"fmt"
	"strings"
	"testing"
)

func TestReportHeader(t *testing.T) {
	cases := []struct {
		name string
		resp *ValidateXmlResponse
		want string
	}{
		{"nil", nil, "validation failed: no validation result available"},
		{"passed", &ValidateXmlResponse{Success: true, ResolvedVesid: "eu.peppol.bis3:invoice:2024.5"},
			"validation passed: no problems reported against eu.peppol.bis3:invoice:2024.5"},
		{"no detail", &ValidateXmlResponse{},
			"validation failed: no problems reported, the validator rejected the document without detail"},
		{"error message", &ValidateXmlResponse{ErrorMessage: "  timed\n  out "},
			"validation failed: no problems reported\nvalidator reported: timed out"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.resp.Report(); got != tc.want {
				t.Errorf("Report() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestReportFindings(t *testing.T) {
	resp := &ValidateXmlResponse{
		ResolvedVesid: "eu.peppol.bis3:invoice:2024.5",
		Results: []*ValidationLayerResult{
			{ValidationType: "xsd", ArtifactId: "peppol/xsd/UBL-Invoice-2.1.xsd"},
			{
				ValidationType: "schematron-xslt2",
				ArtifactId:     `rules\EN16931-UBL-validation.xslt`,
				Errors: []*ValidationError{
					{Level: "ERROR", ErrorID: "BR-01", Message: "[BR-01]-An Invoice shall\n    have a number.", Xpath: "/Invoice[1]", Location: "line 12, col 3"},
				},
				Warnings: []*ValidationError{
					{ErrorID: "BR-02", Location: "line 4"},
				},
			},
		},
	}

	want := `validation failed: 1 error and 1 warning against eu.peppol.bis3:invoice:2024.5

schematron-xslt2 (EN16931-UBL-validation.xslt):
  1) ERROR [BR-01] An Invoice shall have a number.
     at /Invoice[1] (line 12, col 3)
  2) WARN [BR-02] (no message provided)
     at line 4`

	if got := resp.Report(); got != want {
		t.Errorf("Report() =\n%s\nwant\n%s", got, want)
	}
}

func TestReportPassedWithWarnings(t *testing.T) {
	resp := &ValidateXmlResponse{
		Success: true,
		Results: []*ValidationLayerResult{
			{ValidationType: "schematron", Warnings: []*ValidationError{{Level: "warning", ErrorID: "R1", Message: "R1: hint"}}},
		},
	}

	want := "validation passed: 1 warning\n\nschematron:\n  1) WARNING [R1] hint"

	if got := resp.Report(); got != want {
		t.Errorf("Report() =\n%s\nwant\n%s", got, want)
	}
}

func TestReportTruncates(t *testing.T) {
	layer := &ValidationLayerResult{ValidationType: "schematron"}
	for i := range maxProblems + 5 {
		layer.Errors = append(layer.Errors, &ValidationError{ErrorID: fmt.Sprintf("R%d", i), Message: "broken"})
	}

	got := (&ValidateXmlResponse{Results: []*ValidationLayerResult{layer, {ValidationType: "xsd", Errors: []*ValidationError{{Message: "late"}}}}}).Report()

	if !strings.HasPrefix(got, "validation failed: 31 errors") {
		t.Errorf("header does not count every problem:\n%s", got)
	}
	if strings.Contains(got, "26) ") || strings.Contains(got, "\n\nxsd") {
		t.Errorf("report shows more than %d problems:\n%s", maxProblems, got)
	}
	if !strings.HasSuffix(got, "... and 6 more problems not shown") {
		t.Errorf("report does not say what it left out:\n%s", got)
	}
}

func TestProblemText(t *testing.T) {
	cases := []struct {
		id, message, want string
	}{
		{"BR-RO-110", "[BR-RO-110]-Daca Codul", "Daca Codul"},
		{"BR-DE-1", "[BR-DE-1] Eine Rechnung", "Eine Rechnung"},
		{"PEPPOL-EN16931-R001", "PEPPOL-EN16931-R001: Business process MUST", "Business process MUST"},
		{"BR-01", "BR-011 is another rule", "BR-011 is another rule"},
		{"SAX", "cvc-complex-type.2.4.a: Invalid content", "cvc-complex-type.2.4.a: Invalid content"},
		{"BR-01", "[BR-01]", "[BR-01]"},
		{"", "no id at all", "no id at all"},
	}

	for _, tc := range cases {
		if got := problemText(&ValidationError{ErrorID: tc.id, Message: tc.message}); got != tc.want {
			t.Errorf("problemText(%q, %q) = %q, want %q", tc.id, tc.message, got, tc.want)
		}
	}
}
