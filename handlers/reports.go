/* ==========================================================================
   Package handlers — Real-time Dashboard Analytics & GSTR-1 Tax Reporting
   
   WHAT IT IS:
     This module aggregates operational analytics and official Indian GST tax filings:
     - Real-time Dashboard KPIs: Today's sales, bill counts, monthly turnover, total stock
       valuation, procurement expenditure, overheads, and low-stock warning items.
     - GSTR-1 Tax Return Engine: Aggregates sales into official GSTR-1 return tables:
       1. Table 4: B2B Invoices (registered buyers with Customer GSTIN).
       2. Table 7: B2C Small (consumer retail invoices grouped by tax slab rate e.g. 5%).
       3. Table 12: HSN-wise summary of quantities sold, taxable values, and CGST/SGST/IGST.

   HOW IT WORKS:
     - HandleDashboard(): Queries SQLite for current date and month sums using SQL
       aggregations (SUM, COUNT). Compares current stock with low_alert threshold and returns
       KPI metrics in a single lightweight JSON payload.
     - HandleGSTR1Report(): Retrieves all sales invoices in the selected period (or current month).
       Iterates through invoices and line items, classifying registered GSTIN buyers into B2B
       and consumers into B2C Small by tax slab, while tabulating HSN code quantities and values.

   FLOW:
     Client Date Filter -> SQLite Sales Extraction -> GSTR-1 Categorization Engine -> Formatted JSON
   ========================================================================== */

package handlers

import (
	"net/http"
	"siraazhi-pos/db"
	"strings"
)

// HandleDashboard provides KPI cards, low stock alerts, and quick metrics.
// Flow: queries SQLite for today's sales, month turnover, stock value, and low stock items -> returns JSON.
func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats, err := db.GetDashboard()
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load dashboard: "+err.Error())
		return
	}
	JSON(w, http.StatusOK, stats)
}

// GSTR1Summary aggregates B2B, B2C, and HSN tax summary for CA filing
type GSTR1Summary struct {
	B2B []struct {
		CustomerGSTIN string  `json:"customerGstin"`
		CustomerName  string  `json:"customerName"`
		InvoiceNo     string  `json:"invoiceNo"`
		Date          string  `json:"date"`
		InvoiceValue  float64 `json:"invoiceValue"`
		TaxableValue  float64 `json:"taxableValue"`
		CGST          float64 `json:"cgst"`
		SGST          float64 `json:"sgst"`
		IGST          float64 `json:"igst"`
	} `json:"b2b"`
	B2CSmall []struct {
		TaxRate      float64 `json:"taxRate"`
		TaxableValue float64 `json:"taxableValue"`
		CGST         float64 `json:"cgst"`
		SGST         float64 `json:"sgst"`
		IGST         float64 `json:"igst"`
		TotalAmount  float64 `json:"totalAmount"`
	} `json:"b2cSmall"`
	HSNSummary []struct {
		HSN          string  `json:"hsn"`
		Description  string  `json:"description"`
		Qty          float64 `json:"qty"`
		TotalValue   float64 `json:"totalValue"`
		TaxableValue float64 `json:"taxableValue"`
		CGST         float64 `json:"cgst"`
		SGST         float64 `json:"sgst"`
		IGST         float64 `json:"igst"`
	} `json:"hsnSummary"`
	TotalTaxable float64 `json:"totalTaxable"`
	TotalTax     float64 `json:"totalTax"`
	GrandTotal   float64 `json:"grandTotal"`
}

// HandleGSTR1Report aggregates GST tax filing summary for CA/auditor filing.
// Flow: parses date range -> retrieves invoices -> classifies into B2B and B2C -> builds HSN table -> returns JSON.
func HandleGSTR1Report(w http.ResponseWriter, r *http.Request) {
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")

	bills, err := db.GetBills(from, to)
	if err != nil {
		ErrorJSON(w, http.StatusInternalServerError, "Failed to load bills: "+err.Error())
		return
	}

	var report GSTR1Summary
	b2cByRate := make(map[float64]*struct {
		Taxable float64
		CGST    float64
		SGST    float64
		IGST    float64
		Total   float64
	})

	hsnMap := make(map[string]*struct {
		Desc    string
		Qty     float64
		Total   float64
		Taxable float64
		CGST    float64
		SGST    float64
		IGST    float64
	})

	for _, b := range bills {
		discRatio := 1.0
		if b.Subtotal > 0 {
			discRatio = b.GrandTotal / b.Subtotal
		}

		hasGSTIN := strings.TrimSpace(b.Customer.Gstin) != ""
		report.GrandTotal += b.GrandTotal

		// Derive bill taxable & tax for both GST (B2B/B2C) and Non-GST retail bills
		var bTaxable, bGSTAmt, bCGST, bSGST, bIGST float64
		if b.GSTOn {
			bTaxable = b.Taxable
			bGSTAmt = b.GSTAmt
			bCGST = b.CGST
			bSGST = b.SGST
			bIGST = b.IGST
		} else {
			// Normal retail consumer cash bills: derive statutory tax breakdown from item rates
			for _, it := range b.Items {
				itemNetAmt := roundMoney(it.Amount * discRatio)
				rate := it.GSTPct
				var itemNetTax, itemNetTaxable float64
				if rate > 0 {
					itemNetTax = roundMoney(itemNetAmt * rate / (100.0 + rate))
					itemNetTaxable = roundMoney(itemNetAmt - itemNetTax)
				} else {
					itemNetTax = 0
					itemNetTaxable = itemNetAmt
				}
				bTaxable += itemNetTaxable
				bGSTAmt += itemNetTax
				if b.Customer.State != "" && b.Customer.State != "Tamil Nadu" {
					bIGST += itemNetTax
				} else {
					cgst := roundMoney(itemNetTax / 2)
					bCGST += cgst
					bSGST += roundMoney(itemNetTax - cgst)
				}
			}
		}

		report.TotalTaxable += bTaxable
		report.TotalTax += bGSTAmt

		if hasGSTIN {
			// B2B entry (Wholesale Registered Dealers)
			report.B2B = append(report.B2B, struct {
				CustomerGSTIN string  `json:"customerGstin"`
				CustomerName  string  `json:"customerName"`
				InvoiceNo     string  `json:"invoiceNo"`
				Date          string  `json:"date"`
				InvoiceValue  float64 `json:"invoiceValue"`
				TaxableValue  float64 `json:"taxableValue"`
				CGST          float64 `json:"cgst"`
				SGST          float64 `json:"sgst"`
				IGST          float64 `json:"igst"`
			}{
				CustomerGSTIN: b.Customer.Gstin,
				CustomerName:  b.Customer.Name,
				InvoiceNo:     b.BillNo,
				Date:          b.Date,
				InvoiceValue:  roundMoney(b.GrandTotal),
				TaxableValue:  roundMoney(bTaxable),
				CGST:          roundMoney(bCGST),
				SGST:          roundMoney(bSGST),
				IGST:          roundMoney(bIGST),
			})
		} else {
			// B2C Consumer aggregation per item tax rate (prorated by discount)
			for _, it := range b.Items {
				rate := it.GSTPct
				if _, exists := b2cByRate[rate]; !exists {
					b2cByRate[rate] = &struct {
						Taxable float64
						CGST    float64
						SGST    float64
						IGST    float64
						Total   float64
					}{}
				}
				itemNetAmt := roundMoney(it.Amount * discRatio)
				var itemNetTax, itemNetTaxable float64
				if b.GSTOn {
					itemNetTax = roundMoney(it.GSTPortion * discRatio)
					itemNetTaxable = roundMoney(itemNetAmt - itemNetTax)
				} else {
					if rate > 0 {
						itemNetTax = roundMoney(itemNetAmt * rate / (100.0 + rate))
						itemNetTaxable = roundMoney(itemNetAmt - itemNetTax)
					} else {
						itemNetTax = 0
						itemNetTaxable = itemNetAmt
					}
				}

				b2cByRate[rate].Taxable += itemNetTaxable
				b2cByRate[rate].Total += itemNetAmt
				if (b.GSTOn && b.IGST > 0) || (!b.GSTOn && b.Customer.State != "" && b.Customer.State != "Tamil Nadu") {
					b2cByRate[rate].IGST += itemNetTax
				} else {
					cgst := roundMoney(itemNetTax / 2)
					b2cByRate[rate].CGST += cgst
					b2cByRate[rate].SGST += roundMoney(itemNetTax - cgst)
				}
			}
		}

		// HSN breakdown (prorated by discount)
		for _, it := range b.Items {
			hsn := it.HSN
			if hsn == "" {
				hsn = "GENERAL"
			}
			if _, exists := hsnMap[hsn]; !exists {
				hsnMap[hsn] = &struct {
					Desc    string
					Qty     float64
					Total   float64
					Taxable float64
					CGST    float64
					SGST    float64
					IGST    float64
				}{
					Desc: it.NameEn,
				}
			}
			itemNetAmt := roundMoney(it.Amount * discRatio)
			var itemNetTax, itemNetTaxable float64
			if b.GSTOn {
				itemNetTax = roundMoney(it.GSTPortion * discRatio)
				itemNetTaxable = roundMoney(itemNetAmt - itemNetTax)
			} else {
				rate := it.GSTPct
				if rate > 0 {
					itemNetTax = roundMoney(itemNetAmt * rate / (100.0 + rate))
					itemNetTaxable = roundMoney(itemNetAmt - itemNetTax)
				} else {
					itemNetTax = 0
					itemNetTaxable = itemNetAmt
				}
			}

			hsnMap[hsn].Qty += it.Qty
			hsnMap[hsn].Total += itemNetAmt
			hsnMap[hsn].Taxable += itemNetTaxable
			if (b.GSTOn && b.IGST > 0) || (!b.GSTOn && b.Customer.State != "" && b.Customer.State != "Tamil Nadu") {
				hsnMap[hsn].IGST += itemNetTax
			} else {
				cgst := roundMoney(itemNetTax / 2)
				hsnMap[hsn].CGST += cgst
				hsnMap[hsn].SGST += roundMoney(itemNetTax - cgst)
			}
		}
	}

	for rate, val := range b2cByRate {
		report.B2CSmall = append(report.B2CSmall, struct {
			TaxRate      float64 `json:"taxRate"`
			TaxableValue float64 `json:"taxableValue"`
			CGST         float64 `json:"cgst"`
			SGST         float64 `json:"sgst"`
			IGST         float64 `json:"igst"`
			TotalAmount  float64 `json:"totalAmount"`
		}{
			TaxRate:      rate,
			TaxableValue: roundMoney(val.Taxable),
			CGST:         roundMoney(val.CGST),
			SGST:         roundMoney(val.SGST),
			IGST:         roundMoney(val.IGST),
			TotalAmount:  roundMoney(val.Total),
		})
	}

	for hsn, val := range hsnMap {
		report.HSNSummary = append(report.HSNSummary, struct {
			HSN          string  `json:"hsn"`
			Description  string  `json:"description"`
			Qty          float64 `json:"qty"`
			TotalValue   float64 `json:"totalValue"`
			TaxableValue float64 `json:"taxableValue"`
			CGST         float64 `json:"cgst"`
			SGST         float64 `json:"sgst"`
			IGST         float64 `json:"igst"`
		}{
			HSN:          hsn,
			Description:  val.Desc,
			Qty:          val.Qty,
			TotalValue:   roundMoney(val.Total),
			TaxableValue: roundMoney(val.Taxable),
			CGST:         roundMoney(val.CGST),
			SGST:         roundMoney(val.SGST),
			IGST:         roundMoney(val.IGST),
		})
	}

	report.TotalTaxable = roundMoney(report.TotalTaxable)
	report.TotalTax = roundMoney(report.TotalTax)
	report.GrandTotal = roundMoney(report.GrandTotal)

	JSON(w, http.StatusOK, report)
}
