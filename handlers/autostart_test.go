package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestAutoStartLifecycle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Auto-start tests only run on Windows")
	}

	// 1. Test Enabling with "window" mode
	err := SetAutoStart(true, "window")
	if err != nil {
		t.Fatalf("SetAutoStart(true, window) failed: %v", err)
	}

	enabled, mode, cmdStr := QueryAutoStart()
	if !enabled {
		t.Errorf("Expected auto-start to be enabled, got false")
	}
	if mode != "window" {
		t.Errorf("Expected mode 'window', got %s", mode)
	}
	if cmdStr == "" {
		t.Errorf("Expected command string to be populated")
	}

	// 2. Test Switching to "headless" mode
	err = SetAutoStart(true, "headless")
	if err != nil {
		t.Fatalf("SetAutoStart(true, headless) failed: %v", err)
	}

	enabled, mode, _ = QueryAutoStart()
	if !enabled || mode != "headless" {
		t.Errorf("Expected enabled=true, mode=headless, got enabled=%v, mode=%s", enabled, mode)
	}

	// 3. Test Disabling Auto-Start
	err = SetAutoStart(false, "window")
	if err != nil {
		t.Fatalf("SetAutoStart(false) failed: %v", err)
	}

	enabled, _, _ = QueryAutoStart()
	if enabled {
		t.Errorf("Expected auto-start to be disabled, got true")
	}
}

func TestHandleAutoStartHTTP(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Auto-start tests only run on Windows")
	}

	// GET /api/autostart
	req := httptest.NewRequest(http.MethodGet, "/api/autostart", nil)
	w := httptest.NewRecorder()
	HandleAutoStart(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", w.Code)
	}

	var getResp AutoStartResponse
	if err := json.NewDecoder(w.Body).Decode(&getResp); err != nil {
		t.Fatalf("Failed to decode GET response: %v", err)
	}

	// POST /api/autostart (enable = true, mode = window)
	enable := true
	mode := "window"
	bodyBytes, _ := json.Marshal(AutoStartRequest{
		Enabled: &enable,
		Mode:    &mode,
	})

	postReq := httptest.NewRequest(http.MethodPost, "/api/autostart", bytes.NewReader(bodyBytes))
	postReq.RemoteAddr = "127.0.0.1:50000" // Localhost Master auth
	postW := httptest.NewRecorder()
	HandleAutoStart(postW, postReq)

	if postW.Code != http.StatusOK {
		t.Fatalf("Expected status 200 on POST, got %d: %s", postW.Code, postW.Body.String())
	}

	var postResp AutoStartResponse
	if err := json.NewDecoder(postW.Body).Decode(&postResp); err != nil {
		t.Fatalf("Failed to decode POST response: %v", err)
	}
	if !postResp.Enabled {
		t.Errorf("Expected enabled to be true in response")
	}
	if postResp.Mode != "window" {
		t.Errorf("Expected mode 'window', got %s", postResp.Mode)
	}

	// Cleanup
	_ = SetAutoStart(false, "window")
}
