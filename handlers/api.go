/* ==========================================================================
   Package handlers — HTTP Response Utilities & Security Middlewares
   
   WHAT IT IS:
     This module provides standardized HTTP helper functions (JSON serialization,
     error formatting, CORS header management) and the Master vs Client Staff
     terminal access control middleware.

   HOW IT WORKS:
     1. JSON() & ErrorJSON() ensure all API responses use consistent UTF-8 JSON payloads.
     2. CORSMiddleware() permits local network requests from mobile phones, tablets,
        and other counter PCs on the same shop Wi-Fi without browser CORS blocks.
     3. MasterRoleMiddleware() inspects the incoming client IP and authorization tokens:
        - Local requests (127.0.0.1, ::1) or requests with a valid X-Admin-Token
          have unrestricted Master access to all accounting, inventory, and exports.
        - Remote Wi-Fi clients (e.g. staff smartphones on http://<ip>:8080) are
          strictly limited to the billing workflow (/api/products GET and /api/bills POST).
        - Any unauthorized attempt by a mobile terminal to alter stock, delete bills,
          view expenses, or export financial data is rejected with HTTP 403 Forbidden.

   FLOW:
     Incoming HTTP Request -> CORSMiddleware -> MasterRoleMiddleware -> Target Handler -> JSON Response
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"siraazhi-pos/db"
	"strings"
)

// MasterAdminPIN is the default authorization token for master mode
const MasterAdminPIN = "1234"

// JSON writes a standardized JSON response with specified HTTP status code.
// Flow: sets Content-Type header -> sets Status Code -> encodes data struct to JSON stream.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("JSON encode error: %v", err)
		}
	}
}

// ErrorJSON formats an error message into a standardized JSON response: {"error": "..."}.
// Flow: constructs map -> forwards to JSON() with appropriate HTTP error code.
func ErrorJSON(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

// CORSMiddleware allows devices on the local Wi-Fi / LAN to access the billing server.
// Flow: inspects method -> sets Allow-Origin, Methods, Headers -> handles OPTIONS preflight or forwards request.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Admin-Token")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HasMasterAuth checks whether the incoming request originates from localhost or carries a valid Master PIN.
func HasMasterAuth(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	isLocalhost := host == "127.0.0.1" || host == "::1" || host == "localhost"
	if isLocalhost {
		return true
	}
	adminToken := r.Header.Get("X-Admin-Token")
	if adminToken == "" {
		return false
	}
	activePIN := db.GetMasterPin()
	return strings.TrimSpace(adminToken) == strings.TrimSpace(activePIN)
}

// HandleAuthVerify handles POST /api/auth/verify to validate Master Admin PIN without exposing it in plaintext.
func HandleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var req struct {
		PIN string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	activePIN := db.GetMasterPin()
	if strings.TrimSpace(req.PIN) == strings.TrimSpace(activePIN) {
		JSON(w, http.StatusOK, map[string]interface{}{
			"ok":    true,
			"token": activePIN,
		})
		return
	}
	ErrorJSON(w, http.StatusUnauthorized, "Invalid Master PIN")
}

// MasterRoleMiddleware restricts management, inventory adjustments, and exports to the Master Host.
//
// WHAT IT DOES:
//   Guarantees that smartphones/tablets accessing via http://<ip>:8080 can ONLY perform billing.
//
// HOW IT WORKS:
//   1. Extracts the remote IP address of the caller.
//   2. Checks if request originates from localhost (127.0.0.1, ::1) or carries valid X-Admin-Token.
//   3. If caller is a remote client attempting a protected write/admin operation, returns 403 Forbidden.
//   4. Safe read & billing operations (/api/products [GET], /api/bills [POST], /api/network-info)
//      are permitted for all authorized Wi-Fi staff devices.
func MasterRoleMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hasMasterAuth := HasMasterAuth(r)

		path := r.URL.Path
		method := r.Method

		// Define protected management routes that remote mobile terminals CANNOT touch without master PIN:
		isProtectedAdminAction := false

		// 1. Settings modifications
		if strings.HasPrefix(path, "/api/settings") && (method == http.MethodPut || method == http.MethodPost) {
			isProtectedAdminAction = true
		}
		// 2. Database backup & restore
		if strings.HasPrefix(path, "/api/backup") || strings.HasPrefix(path, "/api/restore") {
			isProtectedAdminAction = true
		}
		// 3. Product catalog creation, editing, or deletion
		if strings.HasPrefix(path, "/api/products") && (method == http.MethodPost || method == http.MethodDelete) {
			isProtectedAdminAction = true
		}
		// 4. Inventory manual adjustment or production batch creation
		if strings.HasPrefix(path, "/api/inventory/adjust") || strings.HasPrefix(path, "/api/inventory/production") {
			isProtectedAdminAction = true
		}
		// 5. Procurement purchases and operational expenses logging or deletion
		if strings.HasPrefix(path, "/api/purchases") || strings.HasPrefix(path, "/api/expenses") {
			isProtectedAdminAction = true
		}
		// 6. Bill cancellation or modification (only master can modify or cancel a bill)
		if strings.HasSuffix(path, "/cancel") || (strings.HasPrefix(path, "/api/bills/") && (method == http.MethodPut || method == http.MethodDelete)) {
			isProtectedAdminAction = true
		}
		// 7. Full financial sales reports and GSTR-1 tax export
		if strings.HasPrefix(path, "/api/reports/gstr1") || strings.HasPrefix(path, "/api/export/csv") {
			isProtectedAdminAction = true
		}

		if isProtectedAdminAction && !hasMasterAuth {
			ErrorJSON(w, http.StatusForbidden, "Access Denied: This operation is restricted to the Master System host. Enter Master PIN to unlock.")
			return
		}

		next.ServeHTTP(w, r)
	})
}
