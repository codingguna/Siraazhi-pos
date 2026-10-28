package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
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

	// Test /js/excel.js route
	reqJS := httptest.NewRequest("GET", "/js/excel.js", nil)
	rrJS := httptest.NewRecorder()
	handler.ServeHTTP(rrJS, reqJS)

	ccJS := rrJS.Header().Get("Cache-Control")
	if !strings.Contains(ccJS, "no-cache") || !strings.Contains(ccJS, "no-store") {
		t.Errorf("expected Cache-Control no-cache, no-store for /js/excel.js, got: %q", ccJS)
	}
}

func TestAppJsDOMIDs(t *testing.T) {
	htmlBytes, err := AssembleIndexHTML()
	if err != nil {
		t.Fatalf("AssembleIndexHTML failed: %v", err)
	}
	html := string(htmlBytes)

	appJsBytes, err := readAsset("js/app.js")
	if err != nil {
		t.Fatalf("Failed to read js/app.js: %v", err)
	}
	appJs := string(appJsBytes)

	dollarRegex := regexp.MustCompile(`\$\('([^']+)'\)`)
	matches := dollarRegex.FindAllStringSubmatch(appJs, -1)

	var missing []string
	checked := make(map[string]bool)

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		id := m[1]
		if checked[id] {
			continue
		}
		checked[id] = true

		// Skip dynamic or programmatic IDs generated in runtime
		if strings.HasPrefix(id, "cartItem-") ||
			strings.HasPrefix(id, "billTab-") ||
			strings.HasPrefix(id, "b-") ||
			strings.HasPrefix(id, "btn-") ||
			strings.HasPrefix(id, "del-") ||
			strings.HasPrefix(id, "tab-") ||
			strings.HasPrefix(id, "edit-") ||
			strings.HasPrefix(id, "prod-") ||
			strings.HasPrefix(id, "rev-") ||
			id == "unlockMasterBtn" { // dynamically created in DOM
			continue
		}

		target := `id="` + id + `"`
		if !strings.Contains(html, target) {
			missing = append(missing, id)
		}
	}

	if len(missing) > 0 {
		t.Errorf("The following DOM IDs referenced via $('...') in app.js are missing in assembled HTML: %v", missing)
	}
}

