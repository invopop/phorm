package phorm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
