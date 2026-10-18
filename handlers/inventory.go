/* ==========================================================================
   Package handlers — Inventory, Crushing Batches & Stock Ledger
   
   WHAT IT IS:
     This module manages real-time warehouse inventory and finished goods stock:
     - Manual stock adjustments (recounts, damage, leakage, sampling).
     - Batch crushing production output logging (credit finished bottles/cans to stock).
     - Stock movement ledger history (full audit trail for internal accounting).

   HOW IT WORKS:
     - HandleStockAdjust(): Takes a product ID, positive or negative delta quantity,
       adjustment reason type ('damage', 'recount', 'sampling', 'adjust'), and notes.
       Executes an atomic transaction updating the stock level and recording an audit entry.
     - HandleProduction(): Receives batch crushing output data (e.g., 50 bottles of Coconut Oil
       1L, 20 cans of 5L produced from crushing batch #12). Iterates through items and credits
       inventory while logging batch notes.
     - HandleStockMoves(): Queries the stock_moves audit table to provide a full chronological
       history of sales, cancellations, production runs, and manual adjustments.

   FLOW:
     Client Adjustment / Production Payload -> Validation -> db.AdjustStock() ->
     Atomic SQLite Transaction (UPDATE products stock + INSERT stock_moves) -> JSON Response
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"siraazhi-pos/db"
	"strconv"
	"time"
)

// HandleStockAdjust handles manual stock adjustments (audit, damage, recount).
// Flow: decodes payload -> validates non-zero delta -> invokes db.AdjustStock() -> returns success.
func HandleStockAdjust(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		ProductID string  `json:"productId"`
		Date      string  `json:"date"`
		Qty       float64 `json:"qty"`
		Type      string  `json:"type"` // "adjust", "damage", "recount"
		Note      string  `json:"note"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid adjustment payload")
		return
	}

	if req.ProductID == "" || req.Qty == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Product ID and non-zero quantity required")
		return
	}

	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}
	if req.Type == "" {
		req.Type = "adjust"
	}

	if err := db.AdjustStock(req.ProductID, req.Date, req.Type, req.Qty, req.Note); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Adjustment failed: "+err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// HandleProduction handles crediting produced oil bottles/cans into inventory after crushing.
// Flow: decodes item array -> validates quantities -> updates each SKU's stock -> logs production moves -> returns count.
func HandleProduction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Date  string `json:"date"`
		Note  string `json:"note"`
		Items []struct {
			ProductID string  `json:"productId"`
			Qty       float64 `json:"qty"`
		} `json:"items"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid production payload")
		return
	}

	if len(req.Items) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "At least one product quantity required")
		return
	}

	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}

	addedCount := 0
	for _, it := range req.Items {
		if it.Qty <= 0 {
			continue
		}
		note := req.Note
		if note == "" {
			note = "Crushing production batch"
		}
		if err := db.AdjustStock(it.ProductID, req.Date, "production", it.Qty, note); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, fmt.Sprintf("Failed to add stock for %s: %v", it.ProductID, err))
			return
		}
		addedCount++
	}

	if addedCount == 0 {
		ErrorJSON(w, http.StatusBadRequest, "No positive quantities provided")
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{"ok": true, "itemsProcessed": addedCount})
}

// HandleStockMoves returns the stock ledger history.
// Flow: parses limit query parameter -> queries stock_moves table -> returns chronological ledger entries.
func HandleStockMoves(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	moves, err := db.GetStockMoves(limit)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to retrieve stock moves: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, moves)
}
