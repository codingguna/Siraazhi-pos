/* ==========================================================================
   Package handlers — Purchases, Expenses & Shop Settings Handlers
   
   WHAT IT IS:
     This module manages the business operational ledgers and company profile:
     - Purchases: Logs raw oilseeds procurement (Groundnut, Copra/Coconut, Sesame),
       empty bottle/can stock, and packaging costs.
     - Expenses: Logs recurring overheads (Crushing operator charges, Labour wages,
       Electricity, Transport/freight, Shop rent, Packing consumables).
     - Settings: Manages legal business identity (FSSAI, GSTIN, Company Name, Address),
       bill prefix, financial year, and default printer hardware configurations.

   HOW IT WORKS:
     - HandlePurchases():
         GET: Loads recent material procurement records sorted by date descending.
         POST: Inserts procurement entry into SQLite purchases table.
     - HandleExpenses():
         GET: Loads operational expense records.
         POST: Inserts expense entry into SQLite expenses table.
     - HandleSettings():
         GET: Retrieves JSON business configuration from the settings table.
         PUT/POST: Updates company configuration atomically.

   FLOW:
     HTTP Request -> JSON Validation -> SQLite Query/Mutation -> JSON Status Response
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"net/http"
	"siraazhi-pos/db"
	"strings"
	"time"
)

// HandlePurchases handles GET and POST /api/purchases.
// Flow: GET retrieves procurement records -> encodes to JSON. POST decodes purchase payload -> saves to DB.
func HandlePurchases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := db.GetPurchases()
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to load purchases: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, list)

	case http.MethodPost:
		var p db.Purchase
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid purchase data")
			return
		}
		if p.Item == "" || p.Cost < 0 {
			ErrorJSON(w, http.StatusBadRequest, "Item name and non-negative cost required")
			return
		}
		if p.Date == "" {
			p.Date = time.Now().Format("2006-01-02")
		}
		if p.Unit == "" {
			p.Unit = "kg"
		}
		if err := db.SavePurchase(p); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to save purchase: "+err.Error())
			return
		}
		JSON(w, http.StatusCreated, map[string]bool{"ok": true})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandlePurchaseDelete handles DELETE /api/purchases/{id}.
// Flow: Extracts ID from URL -> deletes from purchases table -> returns success confirmation.
func HandlePurchaseDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/purchases/")
	if id == "" {
		ErrorJSON(w, http.StatusBadRequest, "Missing purchase ID")
		return
	}
	if err := db.DeletePurchase(id); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to delete purchase: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleExpenses handles GET and POST /api/expenses.
// Flow: GET retrieves operational expenses -> encodes to JSON. POST validates amount -> saves to DB.
func HandleExpenses(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := db.GetExpenses()
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to load expenses: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, list)

	case http.MethodPost:
		var e db.Expense
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid expense data")
			return
		}
		if e.Amount <= 0 {
			ErrorJSON(w, http.StatusBadRequest, "Positive amount required")
			return
		}
		if e.Date == "" {
			e.Date = time.Now().Format("2006-01-02")
		}
		if e.Head == "" {
			e.Head = "Other"
		}
		if err := db.SaveExpense(e); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to save expense: "+err.Error())
			return
		}
		JSON(w, http.StatusCreated, map[string]bool{"ok": true})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleExpenseDelete handles DELETE /api/expenses/{id}.
// Flow: Extracts ID from URL -> deletes from expenses table -> returns success confirmation.
func HandleExpenseDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/expenses/")
	if id == "" {
		ErrorJSON(w, http.StatusBadRequest, "Missing expense ID")
		return
	}
	if err := db.DeleteExpense(id); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to delete expense: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleSettings handles GET and PUT /api/settings.
// Flow: GET reads business configuration from DB. PUT/POST updates settings JSON in DB.
func HandleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s, err := db.GetSettings()
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to fetch settings: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, s)

	case http.MethodPut, http.MethodPost:
		var s db.Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid settings data")
			return
		}
		if err := db.SaveSettings(s); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to save settings: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, s)

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
