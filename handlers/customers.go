/* ==========================================================================
   Package handlers — Customer Profile & Search REST Controller
   
   WHAT IT IS:
     Provides instant customer search and auto-completion for POS billing:
     - GET  /api/customers?q=... -> Real-time autocomplete lookup by name/phone/GSTIN
     - POST /api/customers       -> Creates or updates a customer profile directly
   
   HOW IT WORKS:
     1. Search:
        Takes a partial query (e.g. "gu" or "9842").
        Queries SQLite `customers` table with index-accelerated prefix & substring matching.
        Returns up to 15 matching customer cards with name, phone, address, and GSTIN.
     2. Save:
        Upserts customer details into SQLite.
        Unique phone constraint prevents duplicate customer profiles.
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"net/http"
	"siraazhi-pos/db"
	"strconv"
)

// HandleCustomers handles search queries and manual customer updates.
func HandleCustomers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query().Get("q")
		limitStr := r.URL.Query().Get("limit")
		limit := 15
		if limitStr != "" {
			if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
				limit = parsed
			}
		}

		results, err := db.SearchCustomers(q, limit)
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to search customers: "+err.Error())
			return
		}
		if results == nil {
			results = []db.Customer{}
		}
		JSON(w, http.StatusOK, results)

	case http.MethodPost:
		var cust db.Customer
		if err := json.NewDecoder(r.Body).Decode(&cust); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid request JSON: "+err.Error())
			return
		}

		if err := db.SaveCustomer(cust); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to save customer: "+err.Error())
			return
		}

		JSON(w, http.StatusOK, map[string]string{"message": "Customer saved successfully"})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
