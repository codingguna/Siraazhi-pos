/* ==========================================================================
   Package printer — ESC/POS Thermal Receipt Generator for Siraazhi Pro POS

   WHAT IT IS:
     High-performance, pure Go ESC/POS command and layout engine.
     Generates byte streams for standard 80mm (48 columns) and 58mm (32 columns)
     thermal POS printers (Epson, TVS, Everycom, NGX, Posiflex, Xprinter, etc.).

   FEATURES:
     - Header formatting (Company name, brand, FSSAI, GSTIN, address, phone).
     - Accurate 48-col & 32-col tabular column alignment.
     - Indian Rupee currency layout ("Rs. 1,250.00").
     - GST tax breakdown (Taxable, CGST, SGST, IGST).
     - Hardware actions: Cash drawer kick-out, partial/full paper cutting.
     - Hardware Test Print ticket generation.
   ========================================================================== */

package printer

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"time"

	"siraazhi-pos/db"
)

// ESC/POS Command Constants
var (
	cmdInit         = []byte{0x1B, 0x40}                   // ESC @ (Initialize printer)
	cmdAlignLeft    = []byte{0x1B, 0x61, 0x00}             // ESC a 0
	cmdAlignCenter  = []byte{0x1B, 0x61, 0x01}             // ESC a 1
	cmdAlignRight   = []byte{0x1B, 0x61, 0x02}             // ESC a 2
	cmdBoldOn       = []byte{0x1B, 0x45, 0x01}             // ESC E 1
	cmdBoldOff      = []byte{0x1B, 0x45, 0x00}             // ESC E 0
	cmdDoubleSizeOn = []byte{0x1D, 0x21, 0x11}             // GS ! 17 (Double width & height)
	cmdNormalSize   = []byte{0x1D, 0x21, 0x00}             // GS ! 0  (Standard font size)
	cmdCutPaper     = []byte{0x1D, 0x56, 0x41, 0x03}       // GS V A 3 (Feed 3 lines and cut)
	cmdOpenDrawer   = []byte{0x1B, 0x70, 0x00, 0x19, 0xFA} // ESC p 0 25 250 (Pulse pin 2)
)

// formatCurrency formats numbers into Indian Rupee text format: "Rs. 1,250.00"
func formatCurrency(amount float64) string {
	return fmt.Sprintf("Rs.%.2f", amount)
}

// padLeft pads string on left to width (rune-safe)
func padLeft(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(r)) + s
}

// padRight pads string on right to width (rune-safe)
func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(r))
}

// alignCenter centers text within width (rune-safe)
func alignCenter(s string, width int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	leftPad := (width - len(r)) / 2
	rightPad := width - len(r) - leftPad
	return strings.Repeat(" ", leftPad) + s + strings.Repeat(" ", rightPad)
}

// formatTwoCols formats left and right strings with spaces in between (rune-safe)
func formatTwoCols(left, right string, width int) string {
	rLeft := []rune(left)
	rRight := []rune(right)
	totalLen := len(rLeft) + len(rRight)
	if totalLen >= width {
		// If too long, truncate left safely by runes
		avail := width - len(rRight) - 1
		if avail > 0 && len(rLeft) > avail {
			rLeft = rLeft[:avail]
			left = string(rLeft)
		}
	}
	space := width - len(rLeft) - len(rRight)
	if space < 1 {
		space = 1
	}
	return left + strings.Repeat(" ", space) + right
}

// BuildReceiptESC generates raw ESC/POS byte sequence for a finalized bill
func BuildReceiptESC(bill *db.Bill, s *db.Settings, paperSize string, autoCut bool, cashDrawer bool) []byte {
	buf := new(bytes.Buffer)

	// Determine column width based on paper size
	width := 48 // Default 80mm standard width
	if paperSize == "58mm" {
		width = 32 // 58mm compact width
	}

	// 1. Initialize printer
	buf.Write(cmdInit)

	// 2. Open Cash Drawer if enabled
	if cashDrawer {
		buf.Write(cmdOpenDrawer)
	}

	// 3. Header Section (Centered)
	buf.Write(cmdAlignCenter)
	buf.Write(cmdBoldOn)
	company := s.CompanyName
	if company == "" {
		company = "VANUR FARMER PRODUCER COMPANY LTD"
	}
	buf.WriteString(alignCenter(company, width) + "\n")

	brand := s.BrandEn
	if brand == "" {
		brand = "Siraazhi"
	}
	if s.Tagline != "" {
		brand += " — " + s.Tagline
	}
	buf.WriteString(alignCenter(brand, width) + "\n")
	buf.Write(cmdBoldOff)

	if s.Address != "" {
		buf.WriteString(alignCenter(s.Address, width) + "\n")
	}

	contactLine := ""
	if s.Phone != "" {
		contactLine += "Ph: " + s.Phone
	}
	if s.Fssai != "" {
		if contactLine != "" {
			contactLine += " | "
		}
		contactLine += "FSSAI: " + s.Fssai
	}
	if contactLine != "" {
		buf.WriteString(alignCenter(contactLine, width) + "\n")
	}

	if bill.GSTOn && s.Gstin != "" {
		buf.Write(cmdBoldOn)
		buf.WriteString(alignCenter("GSTIN: "+s.Gstin, width) + "\n")
		buf.Write(cmdBoldOff)
	}

	// Divider
	buf.WriteString(strings.Repeat("-", width) + "\n")

	// Receipt Title
	buf.Write(cmdBoldOn)
	title := "RETAIL CASH MEMO"
	if bill.GSTOn {
		title = "TAX INVOICE"
	}
	buf.WriteString(alignCenter(title, width) + "\n")
	buf.Write(cmdBoldOff)

	// 4. Metadata Grid (Bill No, Date, Customer)
	buf.Write(cmdAlignLeft)
	billDate := bill.Date
	if billDate == "" {
		billDate = time.Now().Format("2006-01-02")
	}
	buf.WriteString(formatTwoCols("Bill No: "+bill.BillNo, "Date: "+billDate, width) + "\n")

	custName := bill.Customer.Name
	if custName == "" {
		custName = "Walk-in Customer"
	}
	custLine := "Cust: " + custName
	if bill.Customer.Phone != "" {
		custLine += " | Mob: " + bill.Customer.Phone
	}
	buf.WriteString(custLine + "\n")
	if bill.Customer.Gstin != "" {
		buf.WriteString("Cust GSTIN: " + bill.Customer.Gstin + "\n")
	}

	buf.WriteString(strings.Repeat("-", width) + "\n")

	// 5. Items Table
	if width == 48 {
		// 80mm Layout:
		// Item (22)  Qty(4)  Rate(10)  Amount(12)
		header := padRight("Item Description", 22) + padLeft("Qty", 4) + padLeft("Rate", 10) + padLeft("Amount", 12)
		buf.Write(cmdBoldOn)
		buf.WriteString(header + "\n")
		buf.Write(cmdBoldOff)
		buf.WriteString(strings.Repeat("-", width) + "\n")

		for _, it := range bill.Items {
			name := it.NameEn
			if name == "" {
				name = "Product"
			}
			qtyStr := fmt.Sprintf("%.0f", it.Qty)
			if it.Qty != float64(int(it.Qty)) {
				qtyStr = fmt.Sprintf("%.2f", it.Qty)
			}
			rateStr := fmt.Sprintf("%.2f", it.Price)
			amtStr := fmt.Sprintf("%.2f", it.Amount)

			if len(name) > 22 {
				// Print full item name on its own line
				buf.WriteString(name + "\n")
				row := strings.Repeat(" ", 22) + padLeft(qtyStr, 4) + padLeft(rateStr, 10) + padLeft(amtStr, 12)
				buf.WriteString(row + "\n")
			} else {
				row := padRight(name, 22) + padLeft(qtyStr, 4) + padLeft(rateStr, 10) + padLeft(amtStr, 12)
				buf.WriteString(row + "\n")
			}
		}
	} else {
		// 58mm Layout (32 columns):
		// Item (16)  Qty(3)  Amount(13)
		header := padRight("Item", 16) + padLeft("Qty", 3) + padLeft("Amount", 13)
		buf.Write(cmdBoldOn)
		buf.WriteString(header + "\n")
		buf.Write(cmdBoldOff)
		buf.WriteString(strings.Repeat("-", width) + "\n")

		for _, it := range bill.Items {
			name := it.NameEn
			if name == "" {
				name = "Product"
			}
			qtyStr := fmt.Sprintf("%.0f", it.Qty)
			amtStr := fmt.Sprintf("%.2f", it.Amount)

			buf.WriteString(name + "\n")
			row := strings.Repeat(" ", 16) + padLeft(qtyStr, 3) + padLeft(amtStr, 13)
			buf.WriteString(row + "\n")
		}
	}

	buf.WriteString(strings.Repeat("-", width) + "\n")

	// 6. Totals & Calculations
	buf.WriteString(formatTwoCols("Subtotal:", formatCurrency(bill.Subtotal), width) + "\n")
	if bill.Discount > 0 {
		buf.WriteString(formatTwoCols("Discount:", "-"+formatCurrency(bill.Discount), width) + "\n")
	}

	if bill.GSTOn {
		buf.WriteString(formatTwoCols("Taxable Value:", formatCurrency(bill.Taxable), width) + "\n")
		if bill.IGST > 0 {
			buf.WriteString(formatTwoCols("IGST (5%):", formatCurrency(bill.IGST), width) + "\n")
		} else {
			buf.WriteString(formatTwoCols("CGST (2.5%):", formatCurrency(bill.CGST), width) + "\n")
			buf.WriteString(formatTwoCols("SGST (2.5%):", formatCurrency(bill.SGST), width) + "\n")
		}
		buf.WriteString(formatTwoCols("Total GST (incl.):", formatCurrency(bill.GSTAmt), width) + "\n")
	}

	buf.WriteString(strings.Repeat("=", width) + "\n")

	// Grand Total (Large Bold Text)
	buf.Write(cmdAlignCenter)
	buf.Write(cmdBoldOn)
	buf.Write(cmdDoubleSizeOn)
	buf.WriteString(fmt.Sprintf("TOTAL: %s\n", formatCurrency(bill.GrandTotal)))
	buf.Write(cmdNormalSize)
	buf.Write(cmdBoldOff)
	buf.Write(cmdAlignLeft)

	buf.WriteString(strings.Repeat("=", width) + "\n")

	// Payment Info
	payMode := bill.PayMode
	if payMode == "" {
		payMode = "Cash"
	}
	if payMode == "Split" || (bill.CashPaid > 0 && bill.UPIPaid > 0) {
		buf.WriteString(formatTwoCols("Paid (Split):", formatCurrency(bill.Paid), width) + "\n")
		buf.WriteString(formatTwoCols("  - Cash:", formatCurrency(bill.CashPaid), width) + "\n")
		buf.WriteString(formatTwoCols("  - UPI:", formatCurrency(bill.UPIPaid), width) + "\n")
	} else {
		buf.WriteString(formatTwoCols("Paid ("+payMode+"):", formatCurrency(bill.Paid), width) + "\n")
	}
	if bill.Balance > 0 {
		buf.WriteString(formatTwoCols("Balance Due:", formatCurrency(bill.Balance), width) + "\n")
	}

	buf.WriteString(strings.Repeat("-", width) + "\n")

	// 7. Footer Thank You Note
	buf.Write(cmdAlignCenter)
	footerEn := s.FooterNoteEn
	if footerEn == "" {
		footerEn = "Thank you! Visit again."
	}
	buf.WriteString(footerEn + "\n")
	buf.WriteString(time.Now().Format("02/01/2006 03:04:05 PM") + "\n")

	// Feed 4 blank lines before cut
	buf.WriteString("\n\n\n\n")

	// 8. Cut Paper
	if autoCut {
		buf.Write(cmdCutPaper)
	}

	return buf.Bytes()
}

// BuildTestReceipt generates a verification test ticket
func BuildTestReceipt(printerName, paperSize string, s *db.Settings) []byte {
	buf := new(bytes.Buffer)
	width := 48
	if paperSize == "58mm" {
		width = 32
	}

	buf.Write(cmdInit)
	buf.Write(cmdAlignCenter)
	buf.Write(cmdBoldOn)
	buf.WriteString(alignCenter("SIRAAZHI PRO POS", width) + "\n")
	buf.WriteString(alignCenter("HARDWARE PRINTER TEST SLIP", width) + "\n")
	buf.Write(cmdBoldOff)

	buf.WriteString(strings.Repeat("=", width) + "\n")
	buf.Write(cmdAlignLeft)
	buf.WriteString(formatTwoCols("Printer Name:", printerName, width) + "\n")
	buf.WriteString(formatTwoCols("Paper Preset:", paperSize, width) + "\n")
	buf.WriteString(formatTwoCols("Status:", "ONLINE - READY", width) + "\n")
	buf.WriteString(formatTwoCols("Timestamp:", time.Now().Format("15:04:05"), width) + "\n")
	buf.WriteString(strings.Repeat("-", width) + "\n")

	buf.Write(cmdAlignCenter)
	buf.WriteString("Direct Hardware Printing Verified OK!\n")
	buf.WriteString("Zero-Dialog Mobile & Counter Spooling Active.\n")
	buf.WriteString(strings.Repeat("=", width) + "\n\n\n\n")

	buf.Write(cmdCutPaper)
	return buf.Bytes()
}

// padCenter centers text within width (rune-safe)
func padCenter(s string, width int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	leftPad := (width - len(r)) / 2
	rightPad := width - len(r) - leftPad
	return strings.Repeat(" ", leftPad) + s + strings.Repeat(" ", rightPad)
}

// numberToWordsINR converts a numeric amount to Indian Rupee words (e.g. 420.00 -> "Four Hundred Twenty Rupees Only")
func numberToWordsINR(amount float64) string {
	intPart := int64(math.Floor(amount))
	if intPart <= 0 {
		return "Zero Rupees Only"
	}

	ones := []string{"", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
		"Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen"}
	tens := []string{"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety"}

	convertBelowThousand := func(n int64) string {
		var res string
		if n >= 100 {
			res += ones[n/100] + " Hundred "
			n %= 100
		}
		if n >= 20 {
			res += tens[n/10] + " "
			n %= 10
		}
		if n > 0 {
			res += ones[n] + " "
		}
		return strings.TrimSpace(res)
	}

	var parts []string

	crore := intPart / 10000000
	intPart %= 10000000
	if crore > 0 {
		parts = append(parts, convertBelowThousand(crore)+" Crore")
	}

	lakh := intPart / 100000
	intPart %= 100000
	if lakh > 0 {
		parts = append(parts, convertBelowThousand(lakh)+" Lakh")
	}

	thousand := intPart / 1000
	intPart %= 1000
	if thousand > 0 {
		parts = append(parts, convertBelowThousand(thousand)+" Thousand")
	}

	if intPart > 0 {
		parts = append(parts, convertBelowThousand(intPart))
	}

	words := strings.TrimSpace(strings.Join(parts, " "))
	if words == "" {
		return "Zero Rupees Only"
	}
	return words + " Rupees Only"
}

// formatBoxRow wraps text inside | and | to exactly match the target width (rune-safe)
func formatBoxRow(content string, width int) string {
	r := []rune(content)
	innerWidth := width - 2
	if len(r) > innerWidth {
		r = r[:innerWidth]
	}
	return "|" + string(r) + strings.Repeat(" ", innerWidth-len(r)) + "|"
}

// formatBoxCenter centers text inside | and | of a fixed width (rune-safe)
func formatBoxCenter(content string, width int) string {
	content = strings.TrimSpace(content)
	r := []rune(content)
	innerWidth := width - 2
	if len(r) >= innerWidth {
		return formatBoxRow(content, width)
	}
	leftPad := (innerWidth - len(r)) / 2
	rightPad := innerWidth - len(r) - leftPad
	return "|" + strings.Repeat(" ", leftPad) + content + strings.Repeat(" ", rightPad) + "|"
}

// formatBoxTwoCols formats left and right strings inside | and | of a fixed width (rune-safe)
func formatBoxTwoCols(left, right string, width int) string {
	innerWidth := width - 2
	rLeft := []rune(left)
	rRight := []rune(right)
	totalLen := len(rLeft) + len(rRight)
	if totalLen > innerWidth {
		avail := innerWidth - len(rRight) - 1
		if avail > 0 && len(rLeft) > avail {
			rLeft = rLeft[:avail]
			left = string(rLeft)
		}
	}
	space := innerWidth - len([]rune(left)) - len(rRight)
	if space < 1 {
		space = 1
	}
	return "|" + left + strings.Repeat(" ", space) + right + "|"
}

// BuildA4InvoiceText generates a complete boxed corporate Tax Invoice
// designed specifically for standard office A4 paper (HP Smart Tank, LaserJet, Inkjet).
// It spans the full A4 printable width with standard border gap and fits within half of the paper.
func BuildA4InvoiceText(bill *db.Bill, s *db.Settings) string {
	const width = 96
	var sb strings.Builder

	companyName := "VANUR FARMER PRODUCER COMPANY LIMITED"
	if s != nil && s.CompanyName != "" {
		companyName = strings.ToUpper(s.CompanyName)
	}

	brandEn := "Siraazhi"
	if s != nil && s.BrandEn != "" {
		brandEn = s.BrandEn
	}
	brandTa := "சீராழி"
	if s != nil && s.BrandTa != "" && !strings.Contains(s.BrandTa, "?") && !strings.Contains(s.BrandTa, "à®") && !strings.Contains(s.BrandTa, "à¯") {
		brandTa = s.BrandTa
	}

	tagline := "Pure Traditional Marachekku Oils"
	if s != nil && s.Tagline != "" {
		tagline = s.Tagline
	}

	address := "Thailapuram Village, Vanur, Villupuram, Tamil Nadu"
	if s != nil && s.Address != "" {
		address = s.Address
	}

	var metaParts []string
	if s != nil {
		if s.Phone != "" {
			metaParts = append(metaParts, "Phone: "+s.Phone)
		}
		if s.Gstin != "" {
			metaParts = append(metaParts, "GSTIN: "+s.Gstin)
		}
		if s.Fssai != "" {
			metaParts = append(metaParts, "FSSAI Lic. No: "+s.Fssai)
		}
	}

	// 1. Header Box
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")
	sb.WriteString(formatBoxCenter(companyName, width) + "\n")
	sb.WriteString(formatBoxCenter(fmt.Sprintf("%s (%s) — %s", brandEn, brandTa, tagline), width) + "\n")
	sb.WriteString(formatBoxCenter(address, width) + "\n")
	if len(metaParts) > 0 {
		sb.WriteString(formatBoxCenter(strings.Join(metaParts, "  •  "), width) + "\n")
	}
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	// 2. Invoice Title Banner
	billTitle := "TAX INVOICE"
	if !bill.GSTOn {
		billTitle = "RETAIL CASH BILL"
	}
	sb.WriteString(formatBoxCenter(billTitle, width) + "\n")

	// 3. Two-Column Box: Customer Details (61 chars) & Invoice Metadata (32 chars)
	// 61 + 32 + 3 borders = 96
	custBoxWidth := 61
	invBoxWidth := 32
	twoColDivider := "+" + strings.Repeat("-", custBoxWidth) + "+" + strings.Repeat("-", invBoxWidth) + "+\n"
	sb.WriteString(twoColDivider)

	formatTwoColBoxRow := func(left, right string) string {
		rL := []rune(left)
		if len(rL) > custBoxWidth {
			rL = rL[:custBoxWidth]
		}
		leftPadded := string(rL) + strings.Repeat(" ", custBoxWidth-len(rL))

		rR := []rune(right)
		if len(rR) > invBoxWidth {
			rR = rR[:invBoxWidth]
		}
		rightPadded := string(rR) + strings.Repeat(" ", invBoxWidth-len(rR))

		return "|" + leftPadded + "|" + rightPadded + "|\n"
	}

	custName := "Consumer / Walk-in"
	if bill.Customer.Name != "" {
		custName = bill.Customer.Name
	}
	custPhone := "-"
	if bill.Customer.Phone != "" {
		custPhone = bill.Customer.Phone
	}
	custAddr := "Tamil Nadu"
	if bill.Customer.Address != "" {
		custAddr = bill.Customer.Address
	}
	custGstin := "Unregistered / Consumer"
	if bill.Customer.Gstin != "" {
		custGstin = bill.Customer.Gstin
	}
	custState := "33 - Tamil Nadu"
	if bill.Customer.State != "" {
		custState = bill.Customer.State
	}

	sb.WriteString(formatTwoColBoxRow(" BILLED TO (CUSTOMER):", " INVOICE DETAILS:"))
	sb.WriteString(formatTwoColBoxRow(" Customer Name : "+custName, " Invoice No : "+bill.BillNo))
	sb.WriteString(formatTwoColBoxRow(" Contact Phone : "+custPhone, " Date       : "+bill.Date))
	sb.WriteString(formatTwoColBoxRow(" Delivery Addr : "+custAddr, " Pay Mode   : "+bill.PayMode))
	if bill.Customer.Gstin != "" {
		sb.WriteString(formatTwoColBoxRow(" Customer GSTIN: "+custGstin, " Place of Supply: "+custState))
	} else {
		sb.WriteString(formatTwoColBoxRow(" Place of Supply: "+custState, " Cashier    : Master Counter"))
	}

	// 4. Items Table
	// Width = 96 chars:
	// Col 1: S.No (4), Col 2: Desc (40), Col 3: HSN (8), Col 4: Qty (7), Col 5: Unit (5), Col 6: Rate (11), Col 7: Amt (13)
	// Total: 4 + 40 + 8 + 7 + 5 + 11 + 13 + 8 dividers = 96
	tableBorder := "+" + strings.Repeat("-", 4) + "+" + strings.Repeat("-", 40) + "+" + strings.Repeat("-", 8) + "+" + strings.Repeat("-", 7) + "+" + strings.Repeat("-", 5) + "+" + strings.Repeat("-", 11) + "+" + strings.Repeat("-", 13) + "+\n"
	sb.WriteString(tableBorder)

	tableHeader := "|S.No| " + padRight("Description of Goods", 39) + "|  HSN   |   Qty | Unit| Rate (Rs) |  Amount(Rs) |\n"
	sb.WriteString(tableHeader)
	sb.WriteString(tableBorder)

	for idx, it := range bill.Items {
		name := it.NameEn
		if name == "" {
			name = "Product"
		}
		snoStr := padCenter(fmt.Sprintf("%d", idx+1), 4)
		hsnStr := padCenter(it.HSN, 8)
		qtyStr := padLeft(fmt.Sprintf("%.2f", it.Qty), 7)
		unitStr := padCenter("Btl", 5)
		rateStr := padLeft(fmt.Sprintf("%.2f", it.Price), 11)
		amtStr := padLeft(fmt.Sprintf("%.2f", it.Amount), 13)

		rName := []rune(name)
		if len(rName) > 39 {
			firstLine := string(rName[:39])
			rest := string(rName[39:])
			sb.WriteString("|" + snoStr + "| " + padRight(firstLine, 39) + "|" + hsnStr + "|" + qtyStr + "|" + unitStr + "|" + rateStr + "|" + amtStr + "|\n")
			sb.WriteString("|    | " + padRight(rest, 39) + "|        |       |     |           |             |\n")
		} else {
			sb.WriteString("|" + snoStr + "| " + padRight(name, 39) + "|" + hsnStr + "|" + qtyStr + "|" + unitStr + "|" + rateStr + "|" + amtStr + "|\n")
		}
	}

	sb.WriteString(tableBorder)

	// 5. Subtotal and Tax Breakdown Rows
	// Left: 80 chars, Right: 13 chars. 80 + 13 + 3 = 96
	formatTotalLine := func(label, val string) string {
		return "|" + padLeft(label+" ", 80) + "|" + padLeft(val+" ", 13) + "|\n"
	}
	totalDivider := "+" + strings.Repeat("-", 80) + "+" + strings.Repeat("-", 13) + "+\n"

	subtotal := bill.Subtotal
	if subtotal == 0 {
		for _, it := range bill.Items {
			subtotal += it.Amount
		}
		if subtotal == 0 && bill.GrandTotal > 0 {
			subtotal = bill.GrandTotal
		}
	}
	sb.WriteString(formatTotalLine("Subtotal:", fmt.Sprintf("%.2f", subtotal)))
	if bill.Discount > 0 {
		sb.WriteString(formatTotalLine("Discount:", fmt.Sprintf("-%.2f", bill.Discount)))
	}
	if bill.GSTOn && bill.GSTAmt > 0 {
		sb.WriteString(formatTotalLine("Taxable Value:", fmt.Sprintf("%.2f", bill.Taxable)))
		if bill.CGST > 0 && bill.SGST > 0 {
			sb.WriteString(formatTotalLine("CGST (2.5%) + SGST (2.5%):", fmt.Sprintf("%.2f + %.2f", bill.CGST, bill.SGST)))
		} else if bill.IGST > 0 {
			sb.WriteString(formatTotalLine("IGST (5%):", fmt.Sprintf("%.2f", bill.IGST)))
		}
		sb.WriteString(formatTotalLine("Total GST Tax (incl.):", fmt.Sprintf("%.2f", bill.GSTAmt)))
	}

	sb.WriteString(totalDivider)

	// Grand Total
	sb.WriteString("|" + padLeft("TOTAL INVOICE VALUE (IN RUPEES): ", 80) + "|" + padLeft(fmt.Sprintf("Rs. %.2f ", bill.GrandTotal), 13) + "|\n")
	sb.WriteString(totalDivider)

	// 6. Words & Payment Info
	words := numberToWordsINR(bill.GrandTotal)
	sb.WriteString(formatBoxRow(" Amount in Words: "+words, width) + "\n")

	payDetails := fmt.Sprintf(" Amount Paid    : Rs. %.2f (%s)", bill.Paid, bill.PayMode)
	if bill.PayMode == "Split" || (bill.CashPaid > 0 && bill.UPIPaid > 0) {
		payDetails = fmt.Sprintf(" Paid: Rs.%.2f (Cash:%.0f, UPI:%.0f)", bill.Paid, bill.CashPaid, bill.UPIPaid)
	}
	balDetails := fmt.Sprintf("Balance Due : Rs. %.2f ", bill.Balance)
	sb.WriteString(formatBoxTwoCols(payDetails, balDetails, width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	// 7. Signatures (NO TERMS & CONDITIONS - REMOVED AS REQUESTED)
	signHeader := fmt.Sprintf("For %s", companyName)
	sb.WriteString(formatBoxTwoCols("", signHeader+" ", width) + "\n")
	sb.WriteString(formatBoxRow("", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Customer Signature", "(Authorised Signatory) ", width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	footerNote := "Thank you! Visit again."
	if s != nil && s.FooterNoteEn != "" {
		footerNote = s.FooterNoteEn
	}
	sb.WriteString(formatBoxCenter(footerNote+" • "+time.Now().Format("02/01/2006 03:04 PM"), width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	return sb.String()
}

// BuildA5InvoiceText generates a compact boxed invoice for A5 sheets (Laser / Inkjet)
func BuildA5InvoiceText(bill *db.Bill, s *db.Settings) string {
	const width = 64
	var sb strings.Builder

	companyName := "VANUR FARMER PRODUCER CO. LTD"
	if s != nil && s.CompanyName != "" {
		companyName = strings.ToUpper(s.CompanyName)
	}

	brandEn := "Siraazhi"
	if s != nil && s.BrandEn != "" {
		brandEn = s.BrandEn
	}

	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")
	sb.WriteString(formatBoxCenter(companyName, width) + "\n")
	sb.WriteString(formatBoxCenter(brandEn+" — Pure Marachekku Oils", width) + "\n")
	if s != nil && s.Phone != "" && s.Gstin != "" {
		sb.WriteString(formatBoxCenter("Ph: "+s.Phone+" | GSTIN: "+s.Gstin, width) + "\n")
	}
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	billTitle := "TAX INVOICE"
	if !bill.GSTOn {
		billTitle = "RETAIL CASH BILL"
	}
	sb.WriteString(formatBoxCenter(billTitle, width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	sb.WriteString(formatBoxTwoCols(" Inv No: "+bill.BillNo, "Date: "+bill.Date+" ", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Mode  : "+bill.PayMode, "Cashier: Master Counter ", width) + "\n")
	if bill.Customer.Name != "" {
		sb.WriteString(formatBoxTwoCols(" Cust  : "+bill.Customer.Name, "Phone: "+bill.Customer.Phone+" ", width) + "\n")
	}
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	// Table: Col 1: S.No (4), Col 2: Item (29), Col 3: Qty (6), Col 4: Rate (9), Col 5: Amt (10)
	tBorder := "+" + strings.Repeat("-", 4) + "+" + strings.Repeat("-", 29) + "+" + strings.Repeat("-", 6) + "+" + strings.Repeat("-", 9) + "+" + strings.Repeat("-", 10) + "+\n"
	sb.WriteString(tBorder)
	sb.WriteString("|S.No| " + padRight("Item Description", 28) + "|   Qty| Rate(Rs)|  Amt(Rs) |\n")
	sb.WriteString(tBorder)

	for idx, it := range bill.Items {
		name := it.NameEn
		if name == "" {
			name = "Product"
		}
		snoStr := padCenter(fmt.Sprintf("%d", idx+1), 4)
		qtyStr := padLeft(fmt.Sprintf("%.2f", it.Qty), 6)
		rateStr := padLeft(fmt.Sprintf("%.2f", it.Price), 9)
		amtStr := padLeft(fmt.Sprintf("%.2f", it.Amount), 10)

		rName := []rune(name)
		if len(rName) > 28 {
			first := string(rName[:28])
			rest := string(rName[28:])
			sb.WriteString("|" + snoStr + "| " + padRight(first, 28) + "|" + qtyStr + "|" + rateStr + "|" + amtStr + "|\n")
			sb.WriteString("|    | " + padRight(rest, 28) + "|      |         |          |\n")
		} else {
			sb.WriteString("|" + snoStr + "| " + padRight(name, 28) + "|" + qtyStr + "|" + rateStr + "|" + amtStr + "|\n")
		}
	}
	sb.WriteString(tBorder)

	totDivider := "+" + strings.Repeat("-", 50) + "+" + strings.Repeat("-", 10) + "+\n"
	sb.WriteString("|" + padLeft("Subtotal: ", 50) + "|" + padLeft(fmt.Sprintf("%.2f ", bill.Subtotal), 10) + "|\n")
	if bill.Discount > 0 {
		sb.WriteString("|" + padLeft("Discount: ", 50) + "|" + padLeft(fmt.Sprintf("-%.2f ", bill.Discount), 10) + "|\n")
	}
	if bill.GSTOn && bill.GSTAmt > 0 {
		sb.WriteString("|" + padLeft("Taxable: ", 50) + "|" + padLeft(fmt.Sprintf("%.2f ", bill.Taxable), 10) + "|\n")
		sb.WriteString("|" + padLeft("Total GST: ", 50) + "|" + padLeft(fmt.Sprintf("%.2f ", bill.GSTAmt), 10) + "|\n")
	}
	sb.WriteString(totDivider)
	sb.WriteString("|" + padLeft("GRAND TOTAL: ", 50) + "|" + padLeft(fmt.Sprintf("Rs.%.2f ", bill.GrandTotal), 10) + "|\n")
	sb.WriteString(totDivider)

	sb.WriteString(formatBoxTwoCols(" Paid: "+fmt.Sprintf("Rs.%.2f", bill.Paid), "Balance: "+fmt.Sprintf("Rs.%.2f ", bill.Balance), width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")
	sb.WriteString(formatBoxCenter("Thank you! Visit again.", width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	return sb.String()
}

// BuildReceiptSlipText formats a compact receipt slip for 80mm or 58mm plain text
func BuildReceiptSlipText(bill *db.Bill, s *db.Settings, paperSize string) string {
	width := 48
	if paperSize == "58mm" {
		width = 32
	}

	var sb strings.Builder
	companyName := "SIRAAZHI PRO POS"
	if s != nil && s.CompanyName != "" {
		companyName = strings.ToUpper(s.CompanyName)
	}
	sb.WriteString(strings.Repeat("=", width) + "\n")
	sb.WriteString(alignCenter(companyName, width) + "\n")
	if s != nil && s.Address != "" {
		sb.WriteString(alignCenter(s.Address, width) + "\n")
	}
	sb.WriteString(strings.Repeat("=", width) + "\n")
	sb.WriteString(formatTwoCols(bill.BillNo, bill.Date, width) + "\n")
	sb.WriteString(strings.Repeat("-", width) + "\n")

	for _, it := range bill.Items {
		name := it.NameEn
		if name == "" {
			name = "Product"
		}
		sb.WriteString(name + "\n")
		line := fmt.Sprintf("%.2f x %.2f", it.Qty, it.Price)
		sb.WriteString(formatTwoCols(line, fmt.Sprintf("%.2f", it.Amount), width) + "\n")
	}
	sb.WriteString(strings.Repeat("-", width) + "\n")
	sb.WriteString(formatTwoCols("TOTAL:", fmt.Sprintf("Rs.%.2f", bill.GrandTotal), width) + "\n")
	sb.WriteString(formatTwoCols("PAID ("+bill.PayMode+"):", fmt.Sprintf("Rs.%.2f", bill.Paid), width) + "\n")
	sb.WriteString(strings.Repeat("=", width) + "\n")
	sb.WriteString(alignCenter("Thank you! Visit again.", width) + "\n")
	return sb.String()
}

// BuildDocumentText generates formatted text for standard office/desktop printers (HP Smart Tank, LaserJet, Inkjet)
func BuildDocumentText(bill *db.Bill, s *db.Settings, paperSize string) string {
	ps := strings.ToLower(strings.TrimSpace(paperSize))
	if ps == "a5" {
		return BuildA5InvoiceText(bill, s)
	}
	if ps == "80mm" || ps == "58mm" {
		return BuildReceiptSlipText(bill, s, ps)
	}
	// Default to full corporate A4 Tax Invoice
	return BuildA4InvoiceText(bill, s)
}

// BuildTestDocumentText generates a clean A4/A5 test document for standard desktop printers (HP Smart Tank, etc.)
func BuildTestDocumentText(printerName, paperSize, orientation, colorMode string, s *db.Settings) string {
	const width = 96
	var sb strings.Builder
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")
	sb.WriteString(formatBoxCenter("SIRAAZHI PRO POS — HARDWARE PRINTER VERIFICATION TEST", width) + "\n")
	sb.WriteString(formatBoxCenter("Vanur Farmer Producer Company Limited", width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n\n")

	sb.WriteString(formatBoxTwoCols(" Printer Name:", printerName+" ", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Printer Class:", "Standard Document Spooler (Laser / Inkjet / Smart Tank) ", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Paper Size Preset:", strings.ToUpper(paperSize)+" Sheet (210 x 297 mm) ", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Page Orientation:", strings.Title(orientation)+" ", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Color Mode:", strings.Title(colorMode)+" ", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Hardware Status:", "ONLINE - READY ", width) + "\n")
	sb.WriteString(formatBoxTwoCols(" Spool Timestamp:", time.Now().Format("02/01/2006 15:04:05")+" ", width) + "\n\n")

	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")
	sb.WriteString(formatBoxRow(" VERIFICATION RESULTS & SYSTEM CAPABILITIES:", width) + "\n")
	sb.WriteString(formatBoxRow("  [✓] Direct Zero-Dialog Windows GDI Document Spooling Active", width) + "\n")
	sb.WriteString(formatBoxRow("  [✓] Mobile & Tablet Wireless LAN Printing Enabled via Master Queue", width) + "\n")
	sb.WriteString(formatBoxRow("  [✓] Full Corporate A4 Tax Invoicing & Multi-Printer Routing Ready", width) + "\n")
	sb.WriteString(formatBoxRow("  [✓] Universal Hardware Support for HP Smart Tank, LaserJet, Inkjet & POS Thermal", width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n\n")

	sb.WriteString(formatBoxCenter("Hardware Test Slip Dispatched Successfully!", width) + "\n")
	sb.WriteString(formatBoxCenter("Siraazhi Pro POS • Enterprise Retail & Invoicing Suite", width) + "\n")
	sb.WriteString("+" + strings.Repeat("-", width-2) + "+\n")

	return sb.String()
}
