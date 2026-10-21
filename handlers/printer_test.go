package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlePrintersAPI(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/printers", nil)
	w := httptest.NewRecorder()

	HandlePrinters(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", resp.StatusCode)
	}

	var data struct {
		Printers []string `json:"printers"`
		Count    int      `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if data.Count < 0 {
		t.Errorf("Invalid printer count: %d", data.Count)
	}
	t.Logf("Enumerated %d printers via API", data.Count)
}
