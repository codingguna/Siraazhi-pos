/* ==========================================================================
   Package db — Data Models & Schemas for Siraazhi Pro POS
   
   WHAT IT IS:
     This file defines the domain models and JSON transfer structures for:
     - Company & Shop settings (GSTIN, FSSAI, brand names in English and Tamil).
     - Product catalog (prices, MRP, stock levels, safety thresholds, HSN, barcode).
     - Invoices & line items (taxable base, CGST, SGST, IGST, payment modes).
     - Audit trail stock ledger entries (sales, production, cancellations, adjustments).
     - Raw material procurement purchases and operational overhead expenses.
     - Real-time Dashboard KPI summary data.

   HOW IT WORKS:
     Go structs are decorated with json tags to ensure clean serialization across
     the REST API and SQLite database tables. All monetary figures use float64 rounded
     to 2 decimal places to prevent floating-point calculation drift.

   DATA FLOW:
     SQLite DB Row -> Go Domain Model -> JSON Serialization -> HTTP Response -> Frontend UI
   ========================================================================== */

package db

import "time"

// Settings represents the business and application configuration.
// WHAT IT IS: The master company profile, taxation defaults, and printer preferences.
// HOW IT WORKS: Loaded on startup; can be modified in the Settings tab.
type Settings struct {
	CompanyName    string   `json:"companyName"`
	BrandEn        string   `json:"brandEn"`
	BrandTa        string   `json:"brandTa"`
	Tagline        string   `json:"tagline"`
	Address        string   `json:"address"`
	Phone          string   `json:"phone"`
	Fssai          string   `json:"fssai"`
	Gstin          string   `json:"gstin"`
	GstDefaultOn   bool     `json:"gstDefaultOn"`
	GstDefaultPct  float64  `json:"gstDefaultPct"`
	BillPrefix     string   `json:"billPrefix"`
	FinancialYear  string   `json:"financialYear"`
	FooterNoteEn   string   `json:"footerNoteEn"`
	FooterNoteTa   string   `json:"footerNoteTa"`
	PayModes       []string `json:"payModes"`
	ThermalSize    string   `json:"thermalSize"`    // "80mm" or "58mm"
	DefaultPrinter string   `json:"defaultPrinter"` // "80mm", "58mm", "a4", "a5"
	MasterPin      string   `json:"masterPin"`      // Resettable Master Admin PIN (default "1234")

	// Hardware Printer Configuration (Direct Zero-Dialog Printing & Multi-Printer)
	PrinterInterface          string `json:"printerInterface"`          // "spooler" (Windows USB/driver), "network" (LAN TCP/IP), "browser" (manual dialog)
	PrinterTarget             string `json:"printerTarget"`             // Windows printer name (e.g. "POS-80") or IP ("192.168.1.150")
	PrinterPort               int    `json:"printerPort"`               // Port for network printer (default 9100)
	PrinterType               string `json:"printerType"`               // "thermal" (roll POS) or "document" (A4/A5/Deskjet/Laser)
	PaperSize                 string `json:"paperSize"`                 // "80mm", "58mm", "a4", "a5", "letter"
	Orientation               string `json:"orientation"`               // "portrait" or "landscape"
	ColorMode                 string `json:"colorMode"`                 // "color" or "monochrome"
	ThermalPaperSize          string `json:"thermalPaperSize"`          // "80mm", "58mm"
	AutoCut                   bool   `json:"autoCut"`                   // Feed and cut paper automatically
	OpenCashDrawer            bool   `json:"cashDrawer"`                // Send pulse to cash drawer kick pin
	PrintCopies               int    `json:"printCopies"`               // Number of copies (default 1)
	AutoPrintMobile           bool   `json:"autoPrintMobile"`           // Instantly print mobile/tablet orders without manual approval
	A4PrinterName             string `json:"a4PrinterName"`             // Designated Windows printer for A4 corporate invoices
	SecondaryPrinterEnabled   bool   `json:"secPrinterEnabled"`         // Enable duplicate slip to secondary printer
	SecondaryPrinterInterface string `json:"secPrinterInterface"`       // "spooler" or "network"
	SecondaryPrinterTarget    string `json:"secPrinterTarget"`          // Secondary printer name or IP
	SecondaryPrinterPort      int    `json:"secPrinterPort"`            // Secondary network port
	SecondaryPrinterType      string `json:"secPrinterType"`            // "thermal" or "document"
	SecondaryPaperSize        string `json:"secPaperSize"`              // "80mm", "58mm", "a4", "a5"
	SecondaryOrientation      string `json:"secOrientation"`            // "portrait" or "landscape"
	SecondaryColorMode        string `json:"secColorMode"`              // "color" or "monochrome"
}

// Product represents an individual inventory SKU in the store catalog.
// WHAT IT IS: Single oil SKU (e.g. Coconut Oil 1L, Groundnut Oil 5L, Sesame Oil 500ml).
// HOW IT WORKS: Contains pricing, barcode, tax percentage, and live stock count.
type Product struct {
	ID         string  `json:"id"`
	Category   string  `json:"category"`
	CategoryTa string  `json:"categoryTa"`
	Variant    string  `json:"variant"`
	VariantMl  int     `json:"variantMl"`
	NameEn     string  `json:"nameEn"`
	NameTa     string  `json:"nameTa"`
	HSN        string  `json:"hsn"`
	Barcode    string  `json:"barcode"`
	GSTPct     float64 `json:"gstPct"`
	Price      float64 `json:"price"` // Retail selling rate (inclusive of GST)
	MRP        float64 `json:"mrp"`
	Stock      float64 `json:"stock"`
	LowAlert   float64 `json:"lowAlert"`
	Unit       string  `json:"unit"` // Bottle, Can, Kg, Litre, Pcs, Bag
}

// Customer represents customer details captured on an invoice.
// WHAT IT IS: Retail consumer or registered B2B wholesale buyer.
// HOW IT WORKS: If GSTIN is provided, the bill is classified as B2B in GSTR-1 reports.
type Customer struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
	Gstin   string `json:"gstin"`
	State   string `json:"state"`
}

// BillItem represents a single line item within an invoice.
// WHAT IT IS: Product reference, quantity, unit rate, and calculated line amounts.
// HOW IT WORKS: Line Amount = Qty * Price. GSTPortion is calculated when GST is on.
type BillItem struct {
	ProductID  string  `json:"productId"`
	NameEn     string  `json:"nameEn"`
	NameTa     string  `json:"nameTa"`
	HSN        string  `json:"hsn"`
	Barcode    string  `json:"barcode"`
	Qty        float64 `json:"qty"`
	Price      float64 `json:"price"`
	GSTPct     float64 `json:"gstPct"`
	Amount     float64 `json:"amount"`
	GSTPortion float64 `json:"gstPortion"`
}

// Bill represents a complete sale invoice (Tax Invoice or Cash Bill).
// WHAT IT IS: The immutable header and summary of a finalized counter transaction.
// HOW IT WORKS: Stores total figures, tax breakdowns (CGST/SGST/IGST), payment details, and line items.
type Bill struct {
	ID         string     `json:"id"`
	BillNo     string     `json:"billNo"`
	Date       string     `json:"date"`
	Customer   Customer   `json:"customer"`
	Items      []BillItem `json:"items"`
	Subtotal   float64    `json:"subtotal"`
	Discount   float64    `json:"discount"`
	Taxable    float64    `json:"taxable"`
	GSTOn      bool       `json:"gstOn"`
	GSTAmt     float64    `json:"gstAmt"`
	CGST       float64    `json:"cgst"`
	SGST       float64    `json:"sgst"`
	IGST       float64    `json:"igst"`
	GrandTotal float64    `json:"grandTotal"`
	PayMode    string     `json:"payMode"`
	Paid       float64    `json:"paid"`
	Balance    float64    `json:"balance"`
	Note       string     `json:"note"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// StockMove tracks every inventory change for audit trails and internal reconciliation.
// WHAT IT IS: An immutable ledger of inventory variations.
// HOW IT WORKS: Types include 'sale' (negative), 'production' (positive), 'cancel' (restored), 'adjust' (audit).
type StockMove struct {
	ID        string    `json:"id"`
	Date      string    `json:"date"`
	ProductID string    `json:"productId"`
	Type      string    `json:"type"` // "sale", "cancel", "production", "adjust", "damage"
	Qty       float64   `json:"qty"`
	Note      string    `json:"note"`
	Ref       string    `json:"ref"`
	CreatedAt time.Time `json:"createdAt"`
}

// Purchase logs raw materials, empty bottles, and packaging supplies procurement.
// WHAT IT IS: Seed procurement record from farmers or material vendors.
// HOW IT WORKS: Used to compute monthly raw material expenditure on the dashboard.
type Purchase struct {
	ID        string    `json:"id"`
	Date      string    `json:"date"`
	Supplier  string    `json:"supplier"`
	Item      string    `json:"item"`
	Qty       float64   `json:"qty"`
	Unit      string    `json:"unit"`
	Cost      float64   `json:"cost"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// Expense logs general operational expenditures.
// WHAT IT IS: Day-to-day overhead expenses (Crushing charges, Labour, Electricity, Rent).
// HOW IT WORKS: Aggregated by head and month for net operating cost analysis.
type Expense struct {
	ID        string    `json:"id"`
	Date      string    `json:"date"`
	Head      string    `json:"head"` // Crushing, Labour, Electricity, Transport, Packing, Rent, Other
	Amount    float64   `json:"amount"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// DashboardStats summarizes key operational metrics for the executive overview.
// WHAT IT IS: High-level KPI values displayed on the home dashboard.
// HOW IT WORKS: Calculated in real-time from active SQLite tables.
type DashboardStats struct {
	TodaySales      float64   `json:"todaySales"`
	TodayBillsCount int       `json:"todayBillsCount"`
	MonthSales      float64   `json:"monthSales"`
	MonthBillsCount int       `json:"monthBillsCount"`
	StockValue      float64   `json:"stockValue"`
	TotalProducts   int       `json:"totalProducts"`
	MonthPurchases  float64   `json:"monthPurchases"`
	MonthExpenses   float64   `json:"monthExpenses"`
	LowStockItems   []Product `json:"lowStockItems"`
}
