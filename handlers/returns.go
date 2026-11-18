/* ==========================================================================
   Package handlers — Sales Returns & Refunds Handler
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"siraazhi-pos/db"
	"strings"
	"time"
)

// HandleReturns handles listing all returns and creating a new return.
func HandleReturns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		from := r.URL.Query().Get("from")
		to := r.URL.Query().Get("to")
		returns, err := db.GetReturns(from, to)
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to fetch returns: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, returns)

	case http.MethodPost:
		var req struct {
			BillID     string `json:"billId"`
			RefundMode string `json:"refundMode"` // Cash, UPI, Credit
			Reason     string `json:"reason"`
			Items      []struct {
				ProductID string  `json:"productId"`
				Qty       float64 `json:"qty"`
				Condition string  `json:"condition"` // restock, damaged
			} `json:"items"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
			return
		}

		if strings.TrimSpace(req.BillID) == "" {
			ErrorJSON(w, http.StatusBadRequest, "billId is required")
			return
		}

		if len(req.Items) == 0 {
			ErrorJSON(w, http.StatusBadRequest, "At least one item must be returned")
			return
		}

		// Fetch original bill
		origBill, err := db.GetBillByID(req.BillID)
		if err != nil {
			ErrorJSON(w, http.StatusNotFound, "Original bill not found: "+err.Error())
			return
		}

		// Map original bill items
		origItemMap := make(map[string]db.BillItem)
		for _, it := range origBill.Items {
			origItemMap[it.ProductID] = it
		}

		settings, err := db.GetSettings()
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to load settings: "+err.Error())
			return
		}

		// Generate Credit Note / Return Number
		prefix := "CN"
		fy := settings.FinancialYear
		if fy == "" {
			fy = "2026-27"
		}
		retNo, err := db.NextReturnSeq(prefix, fy)
		if err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to generate return number: "+err.Error())
			return
		}

		var returnItems []db.ReturnItem
		var subtotal float64
		var totalTaxable float64
		var totalGST float64

		shopState := "Tamil Nadu"
		isInterState := origBill.Customer.State != "" && !strings.EqualFold(origBill.Customer.State, shopState)

		today := time.Now().Format("2006-01-02")

		for _, rit := range req.Items {
			if rit.Qty <= 0 {
				continue
			}
			origIt, exists := origItemMap[rit.ProductID]
			if !exists {
				ErrorJSON(w, http.StatusBadRequest, fmt.Sprintf("Product %s was not part of original bill", rit.ProductID))
				return
			}

			// Calculate line amount = qty * price
			lineAmt := roundMoney(rit.Qty * origIt.Price)
			subtotal += lineAmt

			// Calculate taxable and tax portion based on whether original bill was GST
			var lineTaxable, lineTax float64
			if origBill.GSTOn && origIt.GSTPct > 0 {
				taxPortion := roundMoney(lineAmt * (origIt.GSTPct / (100.0 + origIt.GSTPct)))
				lineTaxable = roundMoney(lineAmt - taxPortion)
				lineTax = taxPortion
			} else {
				lineTaxable = lineAmt
				lineTax = 0
			}

			totalTaxable += lineTaxable
			totalGST += lineTax

			cond := strings.ToLower(strings.TrimSpace(rit.Condition))
			if cond != "damaged" {
				cond = "restock"
			}

			returnItems = append(returnItems, db.ReturnItem{
				ProductID: origIt.ProductID,
				NameEn:    origIt.NameEn,
				NameTa:    origIt.NameTa,
				HSN:       origIt.HSN,
				Qty:       rit.Qty,
				Price:     origIt.Price,
				GSTPct:    origIt.GSTPct,
				Amount:    lineAmt,
				Condition: cond,
			})
		}

		if len(returnItems) == 0 {
			ErrorJSON(w, http.StatusBadRequest, "No valid items selected for return")
			return
		}

		var cgst, sgst, igst float64
		if origBill.GSTOn && totalGST > 0 {
			if isInterState {
				igst = totalGST
			} else {
				cgst = roundMoney(totalGST / 2.0)
				sgst = roundMoney(totalGST - cgst)
			}
		}

		refundMode := strings.TrimSpace(req.RefundMode)
		if refundMode == "" {
			refundMode = "Cash"
		}

		newReturn := db.Return{
			ID:          db.GenID("ret"),
			ReturnNo:    retNo,
			BillID:      origBill.ID,
			BillNo:      origBill.BillNo,
			Date:        today,
			Customer:    origBill.Customer,
			Items:       returnItems,
			Subtotal:    roundMoney(subtotal),
			Taxable:     roundMoney(totalTaxable),
			GSTOn:       origBill.GSTOn,
			GSTAmt:      roundMoney(totalGST),
			CGST:        roundMoney(cgst),
			SGST:        roundMoney(sgst),
			IGST:        roundMoney(igst),
			RefundTotal: roundMoney(subtotal),
			RefundMode:  refundMode,
			Reason:      strings.TrimSpace(req.Reason),
		}

		savedReturn, err := db.CreateReturn(newReturn)
		if err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Failed to create return: "+err.Error())
			return
		}

		JSON(w, http.StatusCreated, savedReturn)

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleReturnByID handles fetching a single return or checking remaining refundable quantities.
func HandleReturnByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/returns/")
	id := strings.TrimSpace(path)

	if id == "" {
		ErrorJSON(w, http.StatusBadRequest, "Return ID is required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		ret, err := db.GetReturnByID(id)
		if err != nil {
			ErrorJSON(w, http.StatusNotFound, "Return not found: "+err.Error())
			return
		}
		JSON(w, http.StatusOK, ret)
	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// HandleBillReturnsCheck returns the previously returned quantities for items in a bill.
func HandleBillReturnsCheck(w http.ResponseWriter, r *http.Request) {
	billID := strings.TrimPrefix(r.URL.Path, "/api/bills/")
	billID = strings.TrimSuffix(billID, "/returns")
	billID = strings.TrimSpace(billID)

	if billID == "" {
		ErrorJSON(w, http.StatusBadRequest, "Bill ID is required")
		return
	}

	returned, err := db.GetReturnedQuantitiesByBill(billID)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to get return details: "+err.Error())
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"billId":   billID,
		"returned": returned,
	})
}
