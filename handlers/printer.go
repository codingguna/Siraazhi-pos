/* ==========================================================================
   Package handlers — Hardware Printer Management & Direct Print Endpoints
   
   WHAT IT IS:
     REST API controller for physical hardware printers:
     - GET  /api/printers: Discovers and enumerates all installed Windows printers.
     - POST /api/printers/test: Sends an instant verification test slip to hardware.
     - POST /api/print-direct: Bypasses browser window.print() and sends raw ESC/POS
       directly to the configured counter thermal printer.
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"siraazhi-pos/db"
	"siraazhi-pos/printer"
)

// HandlePrinters handles GET /api/printers
func HandlePrinters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	printers, err := printer.GetInstalledPrinters()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, fmt.Sprintf("Failed to enumerate printers: %v", err))
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"printers": printers,
		"count":    len(printers),
	})
}

// HandlePrinterTest handles POST /api/printers/test
func HandlePrinterTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Interface string `json:"interface"` // "spooler" or "network"
		Target    string `json:"target"`    // Printer Name or IP
		Port      int    `json:"port"`
		PaperSize string `json:"paperSize"` // "80mm" or "58mm"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}

	if req.Target == "" {
		ErrorJSON(w, http.StatusBadRequest, "Printer target name or IP is required")
		return
	}

	settings, _ := db.GetSettings()
	err := printer.PrintTestSlip(req.Interface, req.Target, req.Port, req.PaperSize, &settings)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, fmt.Sprintf("Test print failed: %v", err))
		return
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Hardware test slip sent to %q successfully!", req.Target),
	})
}

// HandlePrintDirect handles POST /api/print-direct
func HandlePrintDirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		BillID string   `json:"billId"`
		Format string   `json:"format"`
		Bill   *db.Bill `json:"bill,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}

	targetBill := req.Bill
	if targetBill == nil || targetBill.ID == "" {
		if req.BillID == "" {
			ErrorJSON(w, http.StatusBadRequest, "billId is required")
			return
		}
		loaded, err := db.GetBillByID(req.BillID)
		if err != nil {
			ErrorJSON(w, http.StatusNotFound, "Bill not found: "+err.Error())
			return
		}
		targetBill = &loaded
	}

	settings, err := db.GetSettings()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load settings: "+err.Error())
		return
	}

	result := printer.PrintBillToHardware(targetBill, req.Format, &settings)
	if !result.Success {
		ErrorJSON(w, http.StatusInternalServerError, result.Message)
		return
	}

	JSON(w, http.StatusOK, result)
}
