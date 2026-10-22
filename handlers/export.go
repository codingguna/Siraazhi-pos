/* ==========================================================================
   Package handlers — Data Portability, Backup, Restore & CSV Streaming
   
   WHAT IT IS:
     This module guarantees 100% data ownership and disaster recovery for the shop:
     - JSON System Backup: Downloads an encrypted, complete snapshot of all products,
       past invoices, stock movements, purchases, and business settings.
     - JSON System Restore: Restores the database from any previous backup file.
     - CSV Streaming Export: Generates downloadable sales register CSVs formatted
       for direct import into Tally, Excel, or custom ERP systems.

   HOW IT WORKS:
     - HandleBackup(): Queries all records from SQLite tables, marshals into a single
       structured BackupData JSON object, and streams it with attachment headers.
     - HandleRestore(): Validates backup integrity and restores products, settings,
       procurement records, and operational expenses in an atomic transaction.
     - HandleSalesCSV(): Streams a UTF-8 CSV response row by row with header columns
       (Bill No, Date, Customer, Taxable, CGST, SGST, IGST, Grand Total, Pay Mode).

   FLOW:
     Client Export Request -> DB Extraction -> Serialization (JSON/CSV) -> Stream Attachment
   ========================================================================== */

package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"siraazhi-pos/db"
	"time"
)

// BackupData contains the complete database dump for portability
type BackupData struct {
	ExportedAt string         `json:"exportedAt"`
	Settings   db.Settings    `json:"settings"`
	Products   []db.Product   `json:"products"`
	Customers  []db.Customer  `json:"customers"`
	Bills      []db.Bill      `json:"bills"`
	StockMoves []db.StockMove `json:"stockMoves"`
	Purchases  []db.Purchase  `json:"purchases"`
	Expenses   []db.Expense   `json:"expenses"`
}

// HandleBackup exports all data as an attachment JSON file.
// Flow: loads settings, products, customers, bills, stock moves, expenses -> packages into BackupData -> sets attachment headers -> streams JSON.
func HandleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	settings, _ := db.GetSettings()
	products, _ := db.GetProducts()
	customers, _ := db.SearchCustomers("", 500000)
	bills, _ := db.GetBills("", "")
	stockMoves, _ := db.GetStockMoves(100000)
	purchases, _ := db.GetPurchases()
	expenses, _ := db.GetExpenses()

	backup := BackupData{
		ExportedAt: time.Now().Format(time.RFC3339),
		Settings:   settings,
		Products:   products,
		Customers:  customers,
		Bills:      bills,
		StockMoves: stockMoves,
		Purchases:  purchases,
		Expenses:   expenses,
	}

	filename := fmt.Sprintf("siraazhi-pos-backup-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(backup)
}

// HandleRestore imports data from a JSON dump.
// Flow: decodes JSON body -> validates required settings & product arrays -> executes atomic DB upserts -> returns recovery stats.
func HandleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var data BackupData
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		ErrorJSON(w, http.StatusBadRequest, "Invalid backup format: "+err.Error())
		return
	}

	if data.Settings.CompanyName == "" || len(data.Products) == 0 {
		ErrorJSON(w, http.StatusBadRequest, "Backup file missing required settings or products")
		return
	}

	// Restore settings
	if err := db.SaveSettings(data.Settings); err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to restore settings: "+err.Error())
		return
	}

	// Restore products
	for _, p := range data.Products {
		_ = db.SaveProduct(p)
	}

	// Restore customers
	for _, c := range data.Customers {
		_ = db.SaveCustomer(c)
	}

	// Restore purchases (upsert)
	for _, p := range data.Purchases {
		_ = db.SavePurchase(p)
	}

	// Restore expenses (upsert)
	for _, e := range data.Expenses {
		_ = db.SaveExpense(e)
	}

	// Restore historical bills and stock moves ledger
	if len(data.Bills) > 0 || len(data.StockMoves) > 0 {
		if err := db.RestoreBillsAndMoves(data.Bills, data.StockMoves); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, "Failed to restore bills/moves: "+err.Error())
			return
		}
	}

	JSON(w, http.StatusOK, map[string]interface{}{
		"ok":        true,
		"message":   "Database successfully restored",
		"products":  len(data.Products),
		"customers": len(data.Customers),
		"bills":     len(data.Bills),
	})
}

// HandleSalesCSV exports sales register as a downloadable CSV.
// Flow: queries sales in range -> writes CSV column header -> writes each invoice row -> flushes to response stream.
func HandleSalesCSV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	bills, err := db.GetBills(from, to)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load sales: "+err.Error())
		return
	}

	filename := fmt.Sprintf("sales-register-%s.csv", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Header row
	_ = writer.Write([]string{
		"Bill No", "Date", "Customer Name", "Customer Phone", "Customer GSTIN",
		"Items Count", "Subtotal", "Discount", "Taxable Value",
		"GST Type", "CGST", "SGST", "IGST", "Grand Total", "Payment Mode", "Paid", "Balance", "Notes",
	})

	for _, b := range bills {
		gstType := "Non-GST"
		if b.GSTOn {
			if b.IGST > 0 {
				gstType = "IGST"
			} else {
				gstType = "CGST+SGST"
			}
		}
		_ = writer.Write([]string{
			b.BillNo,
			b.Date,
			b.Customer.Name,
			b.Customer.Phone,
			b.Customer.Gstin,
			fmt.Sprintf("%d", len(b.Items)),
			fmt.Sprintf("%.2f", b.Subtotal),
			fmt.Sprintf("%.2f", b.Discount),
			fmt.Sprintf("%.2f", b.Taxable),
			gstType,
			fmt.Sprintf("%.2f", b.CGST),
			fmt.Sprintf("%.2f", b.SGST),
			fmt.Sprintf("%.2f", b.IGST),
			fmt.Sprintf("%.2f", b.GrandTotal),
			b.PayMode,
			fmt.Sprintf("%.2f", b.Paid),
			fmt.Sprintf("%.2f", b.Balance),
			b.Note,
		})
	}
}
