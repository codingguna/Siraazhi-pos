package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssembleIndexHTML(t *testing.T) {
	htmlBytes, err := AssembleIndexHTML()
	if err != nil {
		t.Fatalf("AssembleIndexHTML failed: %v", err)
	}

	html := string(htmlBytes)

	// Ensure no unresolved includes remain
	if strings.Contains(html, "<!-- INCLUDE:") {
		t.Errorf("Found unresolved <!-- INCLUDE: --> directive in assembled HTML")
	}

	// Verify all 9 sections are present in assembled output
	expectedSections := []string{
		`id="tab-pos"`,
		`id="tab-dash"`,
		`id="tab-inventory"`,
		`id="tab-production"`,
		`id="tab-purchases"`,
		`id="tab-expenses"`,
		`id="tab-bills"`,
		`id="tab-gstr1"`,
		`id="tab-settings"`,
		`id="productModal"`,
		`id="customerBoxContainer"`,
		`id="printQueueModal"`,
	}

	for _, sec := range expectedSections {
		if !strings.Contains(html, sec) {
			t.Errorf("Missing expected section %s in assembled HTML", sec)
		}
	}
}

func TestHandlerCacheHeaders(t *testing.T) {
	handler := Handler()

	// Test root route "/"
	reqRoot := httptest.NewRequest("GET", "/", nil)
	rrRoot := httptest.NewRecorder()
	handler.ServeHTTP(rrRoot, reqRoot)

	if rrRoot.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /, got %d", rrRoot.Code)
	}
	cc := rrRoot.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") || !strings.Contains(cc, "no-store") {
		t.Errorf("expected Cache-Control no-cache, no-store for /, got: %q", cc)
	}

	// Test /sw.js route
	reqSW := httptest.NewRequest("GET", "/sw.js", nil)
	rrSW := httptest.NewRecorder()
	handler.ServeHTTP(rrSW, reqSW)

	ccSW := rrSW.Header().Get("Cache-Control")
	if !strings.Contains(ccSW, "no-cache") || !strings.Contains(ccSW, "no-store") {
		t.Errorf("expected Cache-Control no-cache, no-store for /sw.js, got: %q", ccSW)
	}
}
