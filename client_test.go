package phorm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateXml(t *testing.T) {
	const body = `{
	  "success": false,
	  "ves": {"vesid": "eu.peppol.bis3:invoice:2024.5"},
	  "results": [
	    {
	      "success": "FALSE",
	      "validity": "invalid",
	      "artifactType": "xsd",
	      "artifactPath": "peppol/CII/xsd/CrossIndustryInvoice.xsd",
	      "items": [
	        {"errorLevel":"ERROR","errorID":"BR-01","errorText":"boom","test":"(cbc:ID) != ''","errorFieldName":"/Invoice[1]","errorLocationObj":{"line":12,"col":3}},
	        {"errorLevel":"WARN","errorID":"BR-02","errorText":"careful","test":"not(cbc:Note)"},
	        {"errorLevel":"SUCCESS","errorText":"ignored"}
	      ]
	    }
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.URL.Path; got != "/api/validate/eu.peppol.bis3:invoice:2024.5" {
			t.Errorf("path = %s", got)
		}
		if got := r.Header.Get("X-Token"); got != "tok" {
			t.Errorf("X-Token = %q, want tok", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/xml" {
			t.Errorf("Content-Type = %q", got)
		}
		payload, _ := io.ReadAll(r.Body)
		if string(payload) != "<xml/>" {
			t.Errorf("body = %q", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	resp, err := c.ValidateXml(context.Background(), &ValidateXmlRequest{
		Vesid:      "eu.peppol.bis3:invoice:2024.5",
		XmlContent: []byte("<xml/>"),
	})
	if err != nil {
		t.Fatalf("ValidateXml: %v", err)
	}
	if resp.Success {
		t.Error("Success = true, want false")
	}
	if resp.ResolvedVesid != "eu.peppol.bis3:invoice:2024.5" {
		t.Errorf("ResolvedVesid = %q", resp.ResolvedVesid)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("Results = %d, want 1", len(resp.Results))
	}
	layer := resp.Results[0]
	if layer.ValidationType != "xsd" {
		t.Errorf("ValidationType = %q", layer.ValidationType)
	}
	if layer.Success {
		t.Error("layer.Success = true, want false (phorm sent success:\"FALSE\"/validity:invalid)")
	}
	if len(layer.Errors) != 1 || len(layer.Warnings) != 1 {
		t.Fatalf("errors=%d warnings=%d, want 1/1", len(layer.Errors), len(layer.Warnings))
	}
	if e := layer.Errors[0]; e.Message != "boom" || e.ErrorID != "BR-01" || e.TestId != "(cbc:ID) != ''" ||
		e.Xpath != "/Invoice[1]" || e.Location != "line 12, col 3" {
		t.Errorf("error mapping wrong: %+v", e)
	}
	if w := layer.Warnings[0]; w.Message != "careful" || w.ErrorID != "BR-02" || w.TestId != "not(cbc:Note)" {
		t.Errorf("warning mapping wrong: %+v", w)
	}
}

// A failing layer makes phive skip the rest, reported as success:"UNDEFINED".
func TestValidateXmlUndefinedTriState(t *testing.T) {
	const body = `{
	  "success": false,
	  "interrupted": true,
	  "results": [
	    {
	      "success": "FALSE",
	      "validity": "invalid",
	      "artifactType": "schematron",
	      "artifactPath": "peppol/sch/PEPPOL-EN16931-UBL.sch",
	      "items": [{"errorLevel":"ERROR","errorText":"BR-CL-01 failed","test":"BR-CL-01"}]
	    },
	    {
	      "success": "UNDEFINED",
	      "validity": "skipped",
	      "artifactType": "edifact",
	      "artifactPath": "peppol/sch/PEPPOL-EN16931-CII.sch",
	      "items": []
	    },
	    {
	      "success": "UNDEFINED",
	      "validity": "unclear",
	      "artifactType": "xsd",
	      "artifactPath": "peppol/CII/xsd/CrossIndustryInvoice.xsd",
	      "items": []
	    }
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").ValidateXml(context.Background(), &ValidateXmlRequest{
		Vesid: "eu.peppol.bis3:invoice:2024.5", XmlContent: []byte("<xml/>"),
	})
	if err != nil {
		t.Fatalf("ValidateXml: %v", err)
	}
	if resp.Success {
		t.Error("Success = true, want false")
	}
	if len(resp.Results) != 3 {
		t.Fatalf("Results = %d, want 3", len(resp.Results))
	}
	for i, layer := range resp.Results {
		if layer.Success {
			t.Errorf("Results[%d].Success = true, want false", i)
		}
	}
	if len(resp.Results[0].Errors) != 1 {
		t.Errorf("Results[0].Errors = %d, want 1", len(resp.Results[0].Errors))
	}
}

func TestDefaultTokenFallback(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"results":[]}`)
	}))
	defer srv.Close()

	// No token supplied -> the client must send phorm's default token.
	c := New(srv.URL, "")
	if _, err := c.ValidateXml(context.Background(), &ValidateXmlRequest{Vesid: "x", XmlContent: []byte("<a/>")}); err != nil {
		t.Fatalf("ValidateXml: %v", err)
	}
	if got != DefaultToken {
		t.Errorf("X-Token = %q, want default %q", got, DefaultToken)
	}
}

func TestValidateXmlHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "bad token")
	}))
	defer srv.Close()

	c := New(srv.URL, "wrong")
	_, err := c.ValidateXml(context.Background(), &ValidateXmlRequest{Vesid: "x", XmlContent: []byte("<a/>")})
	if err == nil {
		t.Fatal("expected error on 403, got nil")
	}
}

func TestListVesIds(t *testing.T) {
	const body = `[
	  {"id":"eu.peppol.bis3:invoice:2024.5","displayName":"Peppol BIS Invoice","deprecated":false},
	  {"id":"eu.peppol.bis3:invoice:2023.5","displayName":"Peppol BIS Invoice old","deprecated":true},
	  {"id":"fr.ctc:cdar:1.3.1","displayName":"FR CDAR","deprecated":false}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include-deprecated") != "true" {
			t.Errorf("include-deprecated not set")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	resp, err := c.ListVesIds(context.Background(), &ListVesIdsRequest{Filter: "peppol"})
	if err != nil {
		t.Fatalf("ListVesIds: %v", err)
	}
	if len(resp.Vesids) != 2 {
		t.Fatalf("filtered vesids = %d, want 2", len(resp.Vesids))
	}
	if resp.Vesids[0].Status != "VALID" || resp.Vesids[1].Status != "DEPRECATED" {
		t.Errorf("status mapping wrong: %+v %+v", resp.Vesids[0], resp.Vesids[1])
	}
}

// A document that breaks a rule comes back as HTTP 400 with the report as the
// body. The findings are the whole point of the call, so they must survive.
func TestValidateXmlFailedValidationIsNotAnError(t *testing.T) {
	const body = `{
	  "success": false,
	  "ves": {"vesid": "eu.peppol.bis3:invoice:2026.5"},
	  "results": [
	    {
	      "success": "FALSE",
	      "validity": "invalid",
	      "artifactType": "schematron",
	      "artifactPath": "openpeppol/2026.5/xslt/PEPPOL-EN16931-UBL.xslt",
	      "items": [
	        {"errorLevel":"ERROR","errorID":"PEPPOL-EN16931-R061","errorText":"Mandate reference MUST be provided for direct debit.","errorFieldName":"cac:PaymentMandate/cbc:ID"}
	      ]
	    }
	  ]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").ValidateXml(context.Background(), &ValidateXmlRequest{
		Vesid: "eu.peppol.bis3:invoice:2026.5", XmlContent: []byte("<Invoice/>"),
	})
	if err != nil {
		t.Fatalf("a failed validation must not be an error: %v", err)
	}
	if resp.Success {
		t.Error("Success = true, want false")
	}
	if len(resp.Results) != 1 || len(resp.Results[0].Errors) != 1 {
		t.Fatalf("findings lost: %+v", resp.Results)
	}
	if got := resp.Results[0].Errors[0].ErrorID; got != "PEPPOL-EN16931-R061" {
		t.Errorf("ErrorID = %q", got)
	}
	if resp.ResolvedVesid != "eu.peppol.bis3:invoice:2026.5" {
		t.Errorf("ResolvedVesid = %q", resp.ResolvedVesid)
	}
}

// A request phorm rejects outright shares the 400 status with a failed
// validation but carries no report, and must still be an error.
func TestValidateXmlRejectedRequestIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unresolvable vesid", http.StatusBadRequest, "The VESID 'no.such:vesid:9.9' could not be resolved."},
		{"body is not xml", http.StatusBadRequest, "Failed to read the message body as XML"},
		{"bad token", http.StatusForbidden, "<!doctype html><html><title>HTTP Status 403</title></html>"},
		{"json that is not a report", http.StatusBadRequest, `{"message":"nope"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := New(srv.URL, "tok").ValidateXml(context.Background(), &ValidateXmlRequest{
				Vesid: "x", XmlContent: []byte("<a/>"),
			})
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

// phorm wraps the VESID list in an object and names the fields `vesid` and
// `name`.
func TestListVesIdsWrappedResponse(t *testing.T) {
	const body = `{
	  "count": 3,
	  "vesids": [
	    {"vesid":"eu.peppol.bis3:invoice:2026.5","name":"OpenPeppol UBL Invoice (2026.5)","deprecated":false},
	    {"vesid":"eu.peppol.bis3:invoice:2023.5","name":"OpenPeppol UBL Invoice (2023.5)","deprecated":true},
	    {"vesid":"fr.ctc:cdar:1.3.1","name":"FR CDAR","deprecated":false}
	  ],
	  "invocationDateTime": "2026-09-11T16:03:04.403Z"
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").ListVesIds(context.Background(), &ListVesIdsRequest{})
	if err != nil {
		t.Fatalf("ListVesIds: %v", err)
	}
	if len(resp.Vesids) != 3 {
		t.Fatalf("vesids = %d, want 3", len(resp.Vesids))
	}
	if got := resp.Vesids[0].Vesid; got != "eu.peppol.bis3:invoice:2026.5" {
		t.Errorf("Vesid = %q, want the id off the wire", got)
	}
	if got := resp.Vesids[0].Name; got != "OpenPeppol UBL Invoice (2026.5)" {
		t.Errorf("Name = %q, want the name off the wire", got)
	}
	if resp.Vesids[1].Status != "DEPRECATED" {
		t.Errorf("Status = %q, want DEPRECATED", resp.Vesids[1].Status)
	}

	// The filter has to see the wrapped field names too.
	resp, err = New(srv.URL, "tok").ListVesIds(context.Background(), &ListVesIdsRequest{Filter: "peppol"})
	if err != nil {
		t.Fatalf("ListVesIds filtered: %v", err)
	}
	if len(resp.Vesids) != 2 {
		t.Fatalf("filtered vesids = %d, want 2", len(resp.Vesids))
	}
}

func TestListVesIdsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()

	if _, err := New(srv.URL, "tok").ListVesIds(context.Background(), &ListVesIdsRequest{}); err == nil {
		t.Fatal("expected an error on 403, got nil")
	}
}

// A synthetic stand-in for a hybrid PDF; the client only passes the bytes on.
var hybridPDF = []byte("%PDF-1.7\n%synthetic test payload\n%%EOF\n")

func TestValidateHybrid(t *testing.T) {
	const body = `{
	  "country": "FR",
	  "documentDetails": {
	    "syntaxID": "cii-d16b",
	    "syntaxVersion": "D16B",
	    "customizationID": "urn:cen.eu:en16931:2017",
	    "vesid": "fr.factur-x:en16931:1.0.8",
	    "profileName": "Factur-X EN 16931"
	  },
	  "success": false,
	  "ves": {"vesid": "fr.factur-x:en16931:1.0.8"},
	  "results": [
	    {
	      "success": "FALSE",
	      "validity": "invalid",
	      "artifactType": "pdf-hybrid-carrier",
	      "artifactPath": "kaltblut/HybridValidator/BR-HYBRID",
	      "items": [
	        {"errorLevel":"ERROR","errorID":"BR-HYBRID-FR-01","errorText":"carrier rule broken","errorFieldName":"/AF"}
	      ]
	    },
	    {
	      "success": "TRUE",
	      "validity": "valid",
	      "artifactType": "pdf-hybrid-carrier",
	      "artifactPath": "kaltblut/HybridValidator/PDF-A3",
	      "items": []
	    },
	    {
	      "success": "FALSE",
	      "validity": "invalid",
	      "artifactType": "hybrid-embedded-xml",
	      "artifactPath": "phorm/HybridValidator/EMBEDDED-XML",
	      "items": [
	        {"errorLevel":"ERROR","errorID":"PHORM-HYBRID-01","errorText":"BT-24 does not match the XMP profile"}
	      ]
	    },
	    {
	      "success": "TRUE",
	      "validity": "valid",
	      "artifactType": "schematron-xslt2",
	      "artifactPath": "factur-x/FACTUR-X_EN16931.xslt",
	      "items": []
	    }
	  ],
	  "invocationDateTime": "2026-11-17T10:00:00Z"
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.URL.Path; got != "/api/hybrid_validate" {
			t.Errorf("path = %s", got)
		}
		if got := r.URL.Query().Get("country"); got != "FR" {
			t.Errorf("country = %q, want FR", got)
		}
		if got := r.Header.Get("X-Token"); got != "tok" {
			t.Errorf("X-Token = %q, want tok", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/pdf" {
			t.Errorf("Content-Type = %q", got)
		}
		payload, _ := io.ReadAll(r.Body)
		if string(payload) != string(hybridPDF) {
			t.Errorf("body = %q", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").ValidateHybrid(context.Background(), &ValidateHybridRequest{
		Country:    "FR",
		PdfContent: hybridPDF,
	})
	if err != nil {
		t.Fatalf("a failed hybrid validation must not be an error: %v", err)
	}
	if resp.Success {
		t.Error("Success = true, want false")
	}
	if resp.Country != "FR" {
		t.Errorf("Country = %q", resp.Country)
	}
	if resp.ResolvedVesid != "fr.factur-x:en16931:1.0.8" {
		t.Errorf("ResolvedVesid = %q", resp.ResolvedVesid)
	}
	d := resp.DocumentDetails
	if d == nil {
		t.Fatal("DocumentDetails = nil")
	}
	if d.CustomizationId != "urn:cen.eu:en16931:2017" || d.SyntaxId != "cii-d16b" ||
		d.Vesid != "fr.factur-x:en16931:1.0.8" || d.ProfileName != "Factur-X EN 16931" {
		t.Errorf("DocumentDetails mapping wrong: %+v", d)
	}
	if len(resp.Results) != 4 {
		t.Fatalf("Results = %d, want 4", len(resp.Results))
	}
	if l := resp.Results[0]; l.ValidationType != "pdf-hybrid-carrier" || l.Success || len(l.Errors) != 1 ||
		l.Errors[0].ErrorID != "BR-HYBRID-FR-01" {
		t.Errorf("carrier layer mapping wrong: %+v", l)
	}
	if l := resp.Results[2]; l.ValidationType != "hybrid-embedded-xml" || len(l.Errors) != 1 ||
		l.Errors[0].ErrorID != "PHORM-HYBRID-01" {
		t.Errorf("embedded xml layer mapping wrong: %+v", l)
	}
	if !resp.Results[3].Success {
		t.Error("Results[3].Success = false, want true")
	}
	if !strings.Contains(resp.Report(), "BR-HYBRID-FR-01") {
		t.Errorf("Report() misses the carrier finding:\n%s", resp.Report())
	}
}

// Without a country the parameter is left off, so phorm falls back to OTHER.
func TestValidateHybridNoCountry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want none", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"results":[{"success":"TRUE","validity":"valid","artifactType":"pdf-hybrid-carrier","items":[]}]}`)
	}))
	defer srv.Close()

	resp, err := New(srv.URL, "tok").ValidateHybrid(context.Background(), &ValidateHybridRequest{PdfContent: hybridPDF})
	if err != nil {
		t.Fatalf("ValidateHybrid: %v", err)
	}
	if !resp.Success {
		t.Error("Success = false, want true")
	}
	if resp.DocumentDetails != nil {
		t.Errorf("DocumentDetails = %+v, want nil", resp.DocumentDetails)
	}
}

// Requests that never produce a report must be errors, including the 200 phorm
// answers with when it cannot parse the PDF at all.
func TestValidateHybridNoReportIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"empty body", http.StatusBadRequest, "The request body is empty"},
		{"not a pdf", http.StatusBadRequest, "The request body is not a PDF document"},
		{"bad token", http.StatusForbidden, "<!doctype html><html><title>HTTP Status 403</title></html>"},
		{"unparseable pdf", http.StatusOK, `{"country":"FR","success":false,"exception":{"class":"java.lang.IllegalStateException","message":"Failed to parse PDF for hybrid validation"},"invocationDateTime":"2026-11-17T10:00:00Z"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			_, err := New(srv.URL, "tok").ValidateHybrid(context.Background(), &ValidateHybridRequest{
				Country: "FR", PdfContent: hybridPDF,
			})
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}
