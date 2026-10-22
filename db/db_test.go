package db

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) string {
	tmpDir, err := os.MkdirTemp("", "siraazhi_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test_pos.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatalf("Failed to init test DB: %v", err)
	}
	return tmpDir
}

func TestBillCreationAndMultiRevision(t *testing.T) {
	tmpDir := setupTestDB(t)
	defer os.RemoveAll(tmpDir)

	// 1. Insert test product with 100 stock
	p := Product{
		ID:         "P_TEST_01",
		Category:   "Oil",
		CategoryTa: "எண்ணெய்",
		NameEn:     "Groundnut Oil 1L",
		NameTa:     "கடலை எண்ணெய் 1L",
		Price:      150.0,
		MRP:        160.0,
		Stock:      100.0,
		GSTPct:     5.0,
		Unit:       "Ltr",
	}
	if err := SaveProduct(p); err != nil {
		t.Fatalf("Failed to save product: %v", err)
	}

	// 2. Create bill with duplicate SKUs in cart (e.g. user scanned barcode twice: 2 + 3 = 5 items)
	billNo, err := NextBillSeq("VFPC", "26-27")
	if err != nil {
		t.Fatalf("Failed to get bill no: %v", err)
	}

	bill := Bill{
		ID:     GenID("b"),
		BillNo: billNo,
		Date:   "2026-10-22",
		Customer: Customer{
			Name:  "Kavitha",
			Phone: "9876543210",
			State: "Tamil Nadu",
		},
		Items: []BillItem{
			{ProductID: p.ID, NameEn: p.NameEn, Qty: 2, Price: 150.0, Amount: 300.0, GSTPct: 5.0},
			{ProductID: p.ID, NameEn: p.NameEn, Qty: 3, Price: 150.0, Amount: 450.0, GSTPct: 5.0},
		},
		Subtotal:   750.0,
		GrandTotal: 750.0,
		Paid:       750.0,
		PayMode:    "Cash",
	}

	created, err := CreateBill(bill)
	if err != nil {
		t.Fatalf("Failed to create bill with duplicate cart items: %v", err)
	}

	// Stock should now be 100 - 5 = 95
	pCheck, err := GetProductByID(p.ID)
	if err != nil {
		t.Fatalf("Failed to get product: %v", err)
	}
	if pCheck.Stock != 95.0 {
		t.Errorf("Expected product stock 95.0, got %.2f", pCheck.Stock)
	}

	// 3. First update on bill: change qty to 10
	created.Items = []BillItem{
		{ProductID: p.ID, NameEn: p.NameEn, Qty: 10, Price: 150.0, Amount: 1500.0, GSTPct: 5.0},
	}
	created.Subtotal = 1500.0
	created.GrandTotal = 1500.0
	created.Paid = 1500.0

	upd1, err := UpdateBill(created)
	if err != nil {
		t.Fatalf("First UpdateBill failed: %v", err)
	}

	pCheck, _ = GetProductByID(p.ID)
	// Stock: 95 was before update. Reversing 5 restored to 100, then deducting 10 gives 90
	if pCheck.Stock != 90.0 {
		t.Errorf("Expected product stock 90.0 after first update, got %.2f", pCheck.Stock)
	}

	// 4. SECOND update on the SAME bill (This used to crash with PRIMARY KEY constraint on sm_upd_%s_%d!)
	upd1.Items = []BillItem{
		{ProductID: p.ID, NameEn: p.NameEn, Qty: 4, Price: 150.0, Amount: 600.0, GSTPct: 5.0},
	}
	upd1.Subtotal = 600.0
	upd1.GrandTotal = 600.0
	upd1.Paid = 600.0

	_, err = UpdateBill(upd1)
	if err != nil {
		t.Fatalf("CRITICAL BUG: Second UpdateBill failed with: %v", err)
	}

	pCheck, _ = GetProductByID(p.ID)
	// Reversing 10 restored to 100, deducting 4 gives 96
	if pCheck.Stock != 96.0 {
		t.Errorf("Expected product stock 96.0 after second update, got %.2f", pCheck.Stock)
	}

	// 5. Cancel bill - stock should restore to 100
	if err := CancelBill(created.ID); err != nil {
		t.Fatalf("CancelBill failed: %v", err)
	}

	pCheck, _ = GetProductByID(p.ID)
	if pCheck.Stock != 100.0 {
		t.Errorf("Expected product stock restored to 100.0 after cancel, got %.2f", pCheck.Stock)
	}
}

func TestSettingsPreservePin(t *testing.T) {
	tmpDir := setupTestDB(t)
	defer os.RemoveAll(tmpDir)

	// Save custom PIN
	s, err := GetSettings()
	if err != nil {
		t.Fatalf("Failed to get initial settings: %v", err)
	}
	s.MasterPin = "9876"
	if err := SaveSettings(s); err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	if GetMasterPin() != "9876" {
		t.Errorf("Expected master PIN '9876', got '%s'", GetMasterPin())
	}

	// Save settings with empty MasterPin (e.g. from staff terminal update)
	s2, _ := GetSettings()
	s2.MasterPin = ""
	if err := SaveSettings(s2); err != nil {
		t.Fatalf("Failed to save settings with empty PIN: %v", err)
	}

	// PIN should STILL be "9876", not wiped or reset to "1234"
	if GetMasterPin() != "9876" {
		t.Errorf("Expected master PIN preserved as '9876', got '%s'", GetMasterPin())
	}
}

func TestAutoDailyBackupAndCloseDB(t *testing.T) {
	tmpDir := setupTestDB(t)
	defer os.RemoveAll(tmpDir)

	// Trigger online VACUUM INTO auto backup
	backupPath, err := AutoDailyBackup(14)
	if err != nil {
		t.Fatalf("AutoDailyBackup failed: %v", err)
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("Expected backup file to exist at %s, got: %v", backupPath, err)
	}

	// Verify close flushes WAL cleanly
	if err := CloseDB(); err != nil {
		t.Fatalf("CloseDB failed: %v", err)
	}
}

func TestInsufficientStockRejection(t *testing.T) {
	tmpDir := setupTestDB(t)
	defer os.RemoveAll(tmpDir)

	p := Product{
		ID:       "P_LOW",
		NameEn:   "Test Item",
		Price:    100.0,
		Stock:    2.0, // Only 2 in stock
		Category: "Test",
	}
	_ = SaveProduct(p)

	// Try to bill 5
	billNo, _ := NextBillSeq("VFPC", "26-27")
	bill := Bill{
		ID:     GenID("b"),
		BillNo: billNo,
		Customer: Customer{
			Name:  "Customer",
			Phone: "9000000000",
		},
		Items: []BillItem{
			{ProductID: p.ID, NameEn: p.NameEn, Qty: 5, Price: 100.0, Amount: 500.0},
		},
		Subtotal:   500.0,
		GrandTotal: 500.0,
		Paid:       500.0,
	}

	_, err := CreateBill(bill)
	if err == nil {
		t.Error("Expected error when requesting more stock than available, got nil")
	}
}
