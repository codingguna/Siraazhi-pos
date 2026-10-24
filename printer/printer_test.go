package printer

import (
	"strings"
	"testing"
	"time"

	"siraazhi-pos/db"
)

func TestBuildReceiptESC(t *testing.T) {
	settings := &db.Settings{
		CompanyName:   "Siraazhi Organic Oil Mills",
		BrandEn:       "Siraazhi Pro",
		BrandTa:       "சீராழி",
		Address:       "123 Mill Road, Kangayam",
		Phone:         "+91 9876543210",
		Gstin:         "33AAAAA0000A1Z5",
		Fssai:         "12423000000000",
		FooterNoteEn:  "Thank you! Visit again.",
		FooterNoteTa:  "நன்றி! மீண்டும் வருக.",
	}

	bill := &db.Bill{
		ID:     "test-bill-1",
		BillNo: "VFPC-2026-0001",
		Date:   "2026-10-21",
		Customer: db.Customer{
			Name:  "Karthik Traders",
			Phone: "9842100000",
			Gstin: "33BBBBB1111B1Z2",
		},
		Items: []db.BillItem{
			{
				ProductID:  "prod-1",
				NameEn:     "Cold Pressed Coconut Oil 1L",
				Qty:        2,
				Price:      260,
				Amount:     520,
				GSTPct:     5,
				GSTPortion: 24.76,
			},
			{
				ProductID:  "prod-2",
				NameEn:     "Wood Pressed Sesame Oil 500ml",
				Qty:        1,
				Price:      180,
				Amount:     180,
				GSTPct:     5,
				GSTPortion: 8.57,
			},
		},
		Subtotal:   700,
		Discount:   0,
		Taxable:    666.67,
		GSTOn:      true,
		GSTAmt:     33.33,
		CGST:       16.67,
		SGST:       16.66,
		GrandTotal: 700,
		PayMode:    "Cash",
		Paid:       700,
		Balance:    0,
		CreatedAt:  time.Now(),
	}

	// Test 80mm ESC/POS generation
	data80 := BuildReceiptESC(bill, settings, "80mm", true, true)
	if len(data80) == 0 {
		t.Fatal("Expected non-empty byte slice for 80mm ESC/POS receipt")
	}

	content80 := string(data80)
	if !strings.Contains(content80, "Siraazhi Organic Oil Mills") {
		t.Errorf("Receipt does not contain company name")
	}
	if !strings.Contains(content80, "VFPC-2026-0001") {
		t.Errorf("Receipt does not contain bill number")
	}
	if !strings.Contains(content80, "Cold Pressed Coconut") {
		t.Errorf("Receipt does not contain line item name")
	}
	if !strings.Contains(content80, "CGST (2.5%):") {
		t.Errorf("Receipt does not contain CGST breakdown")
	}

	// Test 58mm ESC/POS generation
	data58 := BuildReceiptESC(bill, settings, "58mm", true, false)
	if len(data58) == 0 {
		t.Fatal("Expected non-empty byte slice for 58mm ESC/POS receipt")
	}
}

func TestBuildTestSlipESC(t *testing.T) {
	settings := &db.Settings{
		CompanyName: "Siraazhi POS",
		BrandEn:     "Siraazhi Pro",
	}

	slip := BuildTestReceipt("POS-80 (Spooler)", "80mm", settings)
	if len(slip) == 0 {
		t.Fatal("Expected non-empty test slip bytes")
	}

	slipStr := string(slip)
	if !strings.Contains(slipStr, "TEST SLIP") {
		t.Errorf("Test slip does not contain header text")
	}
}

func TestGetInstalledPrinters(t *testing.T) {
	// Should not crash on any system
	printers, err := GetInstalledPrinters()
	if err != nil {
		t.Logf("EnumPrinters note (could be non-fatal on headless CI): %v", err)
	} else {
		t.Logf("Found %d installed printers: %v", len(printers), printers)
	}
}

func TestBuildDocumentText(t *testing.T) {
	settings := &db.Settings{
		CompanyName: "Vanur FPC Ltd",
		BrandEn:     "Siraazhi",
	}

	bill := &db.Bill{
		BillNo: "TEST-A4-001",
		Date:   "2026-10-21",
		Items: []db.BillItem{
			{NameEn: "Coconut Oil", Qty: 2, Price: 250, Amount: 500},
		},
		GrandTotal: 500,
		Paid:       500,
	}

	docA4 := BuildDocumentText(bill, settings, "a4")
	if !strings.Contains(docA4, "TEST-A4-001") || !strings.Contains(docA4, "Coconut Oil") {
		t.Errorf("BuildDocumentText failed for A4")
	}

	// Verify Terms & Conditions and Declarations are completely removed
	if strings.Contains(docA4, "Terms & Conditions") || strings.Contains(docA4, "Declaration") || strings.Contains(docA4, "Goods once sold") {
		t.Errorf("A4 invoice still contains Terms & Conditions / Declaration")
	}

	// Verify signatures and total exist
	if !strings.Contains(docA4, "Customer Signature") || !strings.Contains(docA4, "TOTAL INVOICE VALUE") {
		t.Errorf("A4 invoice missing signature or total value block")
	}

	// Verify line width is 96 chars (+ 1 for \n)
	lines := strings.Split(docA4, "\n")
	if len(lines) > 0 && len(lines[0]) != 96 {
		t.Errorf("Expected A4 box width 96 chars, got %d", len(lines[0]))
	}

	testDoc := BuildTestDocumentText("HP Smart Tank 525", "a4", "portrait", "color", settings)
	if !strings.Contains(testDoc, "HP Smart Tank 525") || !strings.Contains(testDoc, "VERIFICATION RESULTS") {
		t.Errorf("BuildTestDocumentText failed")
	}
}

func TestIsThermalPOSPrinter(t *testing.T) {
	if IsThermalPOSPrinter("HP Smart Tank 520_540 series") {
		t.Errorf("HP Smart Tank should not be classified as a thermal printer")
	}
	if !IsThermalPOSPrinter("POS-80") {
		t.Errorf("POS-80 should be classified as a thermal printer")
	}
	if !IsThermalPOSPrinter("TVS RP 3200") {
		t.Errorf("TVS RP 3200 should be classified as a thermal printer")
	}
}


