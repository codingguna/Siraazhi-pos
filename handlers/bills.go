/* ==========================================================================
   Package handlers — Billing & POS Tax Calculation Handlers
   
   WHAT IT IS:
     This module manages the core point-of-sale invoice operations:
     - Mathematical computation of line item totals, prorated discounts, and tax splits.
     - Dual-mode billing (GST Tax Invoices vs Retail Cash Bills).
     - Automated generation of sequential invoice numbers (e.g. VFPC/2026-27/0001).
     - Atomic database creation of bills and real-time inventory deduction.
     - Reversible cancellation of past invoices with automatic stock recovery.

   HOW IT WORKS:
     1. ComputeBill():
        - Takes line items, discount amount, GST toggle flag, customer state, and shop state.
        - Calculates line totals: qty * price.
        - In retail mode, product prices are treated as tax-inclusive (MRP standard).
        - If GST is enabled, tax portions are extracted using:
            Line Tax Portion = Line Total * (Tax % / (100 + Tax %))
        - If a discount is applied, it discounts the subtotal and prorates the taxable base.
        - Compares Customer State vs Shop State (Tamil Nadu):
            If same state -> intra-state: 50% CGST + 50% SGST.
            If different state -> inter-state: 100% IGST.
     2. HandleBills():
        - POST: Validates stock availability -> generates sequential bill number ->
          creates bill record -> inserts line items -> decrements stock in SQLite.
        - GET: Filters invoices by date range (from / to) and returns JSON list.
     3. HandleBillByID():
        - GET: Retrieves full invoice details including all individual line items.
        - POST (/cancel): Reverses stock deduction, logs cancellation in stockMoves,
          and removes the bill from active sales ledger.

   FLOW:
     Cart Line Items -> ComputeBill() -> Stock Check -> NextBillSeq() ->
     Atomic SQLite Transaction (Insert Bill + Insert Items + Deduct Stock + Log Move) -> Response
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"siraazhi-pos/db"
	"strings"
	"time"
)

// roundMoney rounds a float64 to exactly 2 decimal places.
func roundMoney(val float64) float64 {
	return math.Round(val*100) / 100
}

// ComputeBill computes taxable base, GST splits, discount, and grand total.
//
// WHAT IT IS:
//   Core financial calculator for Indian retail and wholesale tax rules.
//
// HOW IT WORKS:
//   1. Computes line amount = qty * price.
//   2. Extracts embedded GST portion when GST is active.
//   3. Applies customer discount proportionally across items.
//   4. Determines if intra-state (CGST+SGST) or inter-state (IGST).
//   5. Returns subtotal, taxable value, GST amount, CGST, SGST, IGST, and grand total.
func ComputeBill(items []db.BillItem, discount float64, gstOn bool, customerState string, defaultState string) (subtotal, taxable, gstAmt, cgst, sgst, igst, grandTotal float64, processed []db.BillItem) {
	subtotal = 0
	totalTaxPortion := 0.0

	for i := range items {
		it := items[i]
		lineTotal := roundMoney(it.Qty * it.Price)
		it.Amount = lineTotal
		subtotal = roundMoney(subtotal + lineTotal)

		if gstOn && it.GSTPct > 0 {
			// Retail standard: Price is inclusive of GST
			taxPortion := roundMoney(lineTotal * it.GSTPct / (100 + it.GSTPct))
			it.GSTPortion = taxPortion
			totalTaxPortion = roundMoney(totalTaxPortion + taxPortion)
		} else {
			it.GSTPortion = 0
		}
		processed = append(processed, it)
	}

	discount = roundMoney(discount)
	afterDisc := roundMoney(subtotal - discount)
	if afterDisc < 0 {
		afterDisc = 0
	}

	grandTotal = afterDisc

	if gstOn && subtotal > 0 {
		// Prorate GST after discount
		ratio := afterDisc / subtotal
		gstAmt = roundMoney(totalTaxPortion * ratio)
		taxable = roundMoney(afterDisc - gstAmt)

		// Check if inter-state (IGST) or intra-state (CGST + SGST)
		// Default state is Tamil Nadu (33)
		isInterState := false
		if customerState != "" && defaultState != "" && !strings.EqualFold(strings.TrimSpace(customerState), strings.TrimSpace(defaultState)) {
			isInterState = true
		}

		if isInterState {
			igst = gstAmt
			cgst = 0
			sgst = 0
		} else {
			cgst = roundMoney(gstAmt / 2)
			sgst = roundMoney(gstAmt - cgst)
			igst = 0
		}
	} else {
		gstAmt = 0
		cgst = 0
		sgst = 0
		igst = 0
		taxable = afterDisc
	}

	return subtotal, taxable, gstAmt, cgst, sgst, igst, grandTotal, processed
}

// HandleBills handles GET /api/bills (list sales) and POST /api/bills (create bill).
//
// FLOW FOR POST:
//   Receive JSON payload -> Validate payload & item count -> Load shop settings ->
//   Run ComputeBill() -> Generate Bill Sequence -> Execute atomic CreateBill() -> Return Bill JSON.
//
// FLOW FOR GET:
//   Parse 'from' and 'to' query parameters -> Query SQLite bills table -> Attach line items -> Return JSON.
func HandleBills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		bills, err := db.GetBills(from, to)
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to retrieve bills: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, bills)

	case http.MethodPost:
		var req struct {
			Date     string        `json:"date"`
			Customer db.Customer   `json:"customer"`
			Items    []db.BillItem `json:"items"`
			Discount float64       `json:"discount"`
			GSTOn    *bool         `json:"gstOn"`
			PayMode  string        `json:"payMode"`
			Paid     *float64      `json:"paid"`
			Note     string        `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid bill payload: "+err.Error())
			return
		}

		if len(req.Items) == 0 {
			ErrorJSON(w, http.StatusBadRequest, "Bill must contain at least one item")
			return
		}

		settings, err := db.GetSettings()
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to read settings: "+err.Error())
			return
		}

		gstOn := settings.GstDefaultOn
		if req.GSTOn != nil {
			gstOn = *req.GSTOn
		}

		date := req.Date
		if date == "" {
			date = time.Now().Format("2006-01-02")
		}

		payMode := req.PayMode
		if payMode == "" {
			payMode = "Cash"
		}

		// Calculate bill financial figures
		subtotal, taxable, gstAmt, cgst, sgst, igst, grandTotal, processedItems := ComputeBill(
			req.Items, req.Discount, gstOn, req.Customer.State, "Tamil Nadu",
		)

		paid := grandTotal
		if req.Paid != nil {
			paid = *req.Paid
		}
		balance := roundMoney(grandTotal - paid)

		// Generate sequential bill number atomically
		billNo, err := db.NextBillSeq(settings.BillPrefix, settings.FinancialYear)
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to generate bill number: "+err.Error())
			return
		}

		billID := fmt.Sprintf("b_%d", time.Now().UnixNano()%100000000)
		bill := db.Bill{
			ID:         billID,
			BillNo:     billNo,
			Date:       date,
			Customer:   req.Customer,
			Items:      processedItems,
			Subtotal:   subtotal,
			Discount:   req.Discount,
			Taxable:    taxable,
			GSTOn:      gstOn,
			GSTAmt:     gstAmt,
			CGST:       cgst,
			SGST:       sgst,
			IGST:       igst,
			GrandTotal: grandTotal,
			PayMode:    payMode,
			Paid:       paid,
			Balance:    balance,
			Note:       req.Note,
			CreatedAt:  time.Now(),
		}

		savedBill, err := db.CreateBill(bill)
		if err != nil {
			ErrorJSON(w, http.StatusBadRequest, err.Error())
			return
		}

		JSON(w, http.StatusCreated, savedBill)

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleBillByID handles GET /api/bills/{id} and POST /api/bills/{id}/cancel.
//
// FLOW FOR CANCEL:
//   Extract bill ID -> Fetch line items -> Restore product stock -> Log 'cancel' in stockMoves ->
//   Delete invoice from bills table -> Return success confirmation.
func HandleBillByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/bills/")
	parts := strings.Split(path, "/")
	id := parts[0]
	if id == "" {
		ErrorJSON(w, http.StatusBadRequest, "Missing bill ID")
		return
	}

	if len(parts) > 1 && parts[1] == "cancel" {
		if r.Method != http.MethodPost {
			ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		if err := db.CancelBill(id); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to cancel bill: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	if r.Method != http.MethodGet {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	bill, err := db.GetBillByID(id)
	if err != nil {
		ErrorJSON(w, http.StatusNotFound, "Bill not found")
		return
	}
	JSON(w, http.StatusOK, bill)
}
