package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"siraazhi-pos/db"
	"strings"
	"testing"
)

func setupTestDBForHandlers(t *testing.T) string {
	tmpDir, err := os.MkdirTemp("", "siraazhi_h_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test_handlers.db")
	if err := db.InitDB(dbPath); err != nil {
		t.Fatalf("Failed to init test DB: %v", err)
	}
	return tmpDir
}

func TestComputeBill_IntraAndInterState(t *testing.T) {
	items := []db.BillItem{
		{
			ProductID: "P1",
			Qty:       2,
			Price:     105.0, // 5% GST included -> taxable 100, gst 5 per unit = 10 total
			GSTPct:    5.0,
		},
	}

	// Case 1: Intra-state (buyer is in Tamil Nadu, default shop is in Tamil Nadu)
	subtotal, taxable, gstAmt, cgst, sgst, igst, grandTotal, _ := ComputeBill(items, 0, true, "Tamil Nadu", "Tamil Nadu")

	if grandTotal != 210.0 {
		t.Errorf("Expected grand total 210, got %.2f", grandTotal)
	}
	if subtotal != 210.0 {
		t.Errorf("Expected subtotal 210, got %.2f", subtotal)
	}
	if gstAmt != 10.0 {
		t.Errorf("Expected gstAmt 10, got %.2f", gstAmt)
	}
	if cgst != 5.0 || sgst != 5.0 {
		t.Errorf("Expected CGST 5.0 and SGST 5.0, got CGST=%.2f, SGST=%.2f", cgst, sgst)
	}
	if igst != 0.0 {
		t.Errorf("Expected IGST 0.0 for intra-state, got %.2f", igst)
	}
	if taxable != 200.0 {
		t.Errorf("Expected taxable 200, got %.2f", taxable)
	}

	// Case 2: Inter-state (buyer is in Karnataka, default shop is in Tamil Nadu)
	_, _, gstAmt2, cgst2, sgst2, igst2, _, _ := ComputeBill(items, 0, true, "Karnataka", "Tamil Nadu")

	if gstAmt2 != 10.0 {
		t.Errorf("Expected gstAmt 10, got %.2f", gstAmt2)
	}
	if igst2 != 10.0 {
		t.Errorf("Expected IGST 10.0 for inter-state, got %.2f", igst2)
	}
	if cgst2 != 0.0 || sgst2 != 0.0 {
		t.Errorf("Expected CGST=0 and SGST=0 for inter-state, got CGST=%.2f, SGST=%.2f", cgst2, sgst2)
	}

	// Case 3: Discount prorating
	// Total 210, discount 105 (50% off) -> net 105, GST should be 5
	_, _, gstAmt3, _, _, _, grandTotal3, _ := ComputeBill(items, 105.0, true, "Tamil Nadu", "Tamil Nadu")
	if grandTotal3 != 105.0 {
		t.Errorf("Expected grand total 105, got %.2f", grandTotal3)
	}
	if gstAmt3 != 5.0 {
		t.Errorf("Expected prorated GST 5.0, got %.2f", gstAmt3)
	}
}

func TestAuthVerifyEndpoint(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	// Set master pin to 5678
	s, err := db.GetSettings()
	if err != nil {
		t.Fatalf("Failed to get settings: %v", err)
	}
	s.MasterPin = "5678"
	_ = db.SaveSettings(s)

	// Test with wrong PIN
	wrongBody, _ := json.Marshal(map[string]string{"pin": "0000"})
	reqWrong := httptest.NewRequest("POST", "/api/auth/verify", bytes.NewReader(wrongBody))
	recWrong := httptest.NewRecorder()
	HandleAuthVerify(recWrong, reqWrong)

	if recWrong.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for wrong pin, got %d", recWrong.Code)
	}

	// Test with correct PIN
	correctBody, _ := json.Marshal(map[string]string{"pin": "5678"})
	reqRight := httptest.NewRequest("POST", "/api/auth/verify", bytes.NewReader(correctBody))
	recRight := httptest.NewRecorder()
	HandleAuthVerify(recRight, reqRight)

	if recRight.Code != http.StatusOK {
		t.Errorf("Expected 200 for correct pin, got %d", recRight.Code)
	}

	var res map[string]interface{}
	_ = json.NewDecoder(recRight.Body).Decode(&res)
	if res["ok"] != true {
		t.Errorf("Expected ok: true, got %v", res["ok"])
	}
}

func TestHasMasterAuth(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	s, _ := db.GetSettings()
	s.MasterPin = "4321"
	_ = db.SaveSettings(s)

	// Localhost remote address
	reqLocal := httptest.NewRequest("GET", "/api/settings", nil)
	reqLocal.RemoteAddr = "127.0.0.1:54321"
	if !HasMasterAuth(reqLocal) {
		t.Error("Expected HasMasterAuth true for 127.0.0.1")
	}

	// Remote IP without token
	reqRemote := httptest.NewRequest("GET", "/api/settings", nil)
	reqRemote.RemoteAddr = "192.168.1.50:54321"
	if HasMasterAuth(reqRemote) {
		t.Error("Expected HasMasterAuth false for remote IP without token")
	}

	// Remote IP with wrong token
	reqRemote.Header.Set("X-Admin-Token", "9999")
	if HasMasterAuth(reqRemote) {
		t.Error("Expected HasMasterAuth false for remote IP with invalid token")
	}

	// Remote IP with valid token
	reqRemote.Header.Set("X-Admin-Token", "4321")
	if !HasMasterAuth(reqRemote) {
		t.Error("Expected HasMasterAuth true for remote IP with valid X-Admin-Token")
	}
}

func TestHandleBills_CreateAndCancel(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	// Save test product with 50 stock
	p := db.Product{
		ID:       "P_API_01",
		NameEn:   "Gingelly Oil 1L",
		Price:    200.0,
		Stock:    50.0,
		Category: "Oil",
		GSTPct:   5.0,
	}
	_ = db.SaveProduct(p)

	// 1. Test POST /api/bills with empty items (should fail 400)
	bodyEmpty, _ := json.Marshal(map[string]interface{}{
		"items": []interface{}{},
	})
	req1 := httptest.NewRequest("POST", "/api/bills", bytes.NewReader(bodyEmpty))
	rec1 := httptest.NewRecorder()
	HandleBills(rec1, req1)
	if rec1.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty items, got %d", rec1.Code)
	}

	// 2. Test POST /api/bills with valid bill
	validPayload := map[string]interface{}{
		"date": "2026-10-22",
		"customer": map[string]string{
			"name":  "Malar",
			"phone": "9876501234",
			"state": "Tamil Nadu",
		},
		"items": []map[string]interface{}{
			{
				"productId": p.ID,
				"nameEn":    p.NameEn,
				"qty":       3,
				"price":     200.0,
				"gstPct":    5.0,
			},
		},
		"discount": 0.0,
		"payMode":  "Cash",
	}
	bodyValid, _ := json.Marshal(validPayload)
	req2 := httptest.NewRequest("POST", "/api/bills", bytes.NewReader(bodyValid))
	rec2 := httptest.NewRecorder()
	HandleBills(rec2, req2)

	if rec2.Code != http.StatusCreated && rec2.Code != http.StatusOK {
		t.Fatalf("Expected 201 Created for valid bill creation, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var createdBill db.Bill
	if err := json.NewDecoder(rec2.Body).Decode(&createdBill); err != nil {
		t.Fatalf("Failed to decode created bill: %v", err)
	}
	if createdBill.GrandTotal != 600.0 {
		t.Errorf("Expected grand total 600.0, got %.2f", createdBill.GrandTotal)
	}

	// Verify stock is now 50 - 3 = 47
	pCheck, _ := db.GetProductByID(p.ID)
	if pCheck.Stock != 47.0 {
		t.Errorf("Expected stock 47.0, got %.2f", pCheck.Stock)
	}

	// 3. Test GET /api/bills
	reqGet := httptest.NewRequest("GET", "/api/bills?from=2026-10-01&to=2026-10-31", nil)
	recGet := httptest.NewRecorder()
	HandleBills(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Errorf("Expected 200 for GET bills, got %d", recGet.Code)
	}

	// 4. Test POST /api/bills/{id}/cancel with invalid PIN
	cancelPayloadWrong, _ := json.Marshal(map[string]string{"pin": "0000"})
	reqCancelWrong := httptest.NewRequest("POST", "/api/bills/"+createdBill.ID+"/cancel", bytes.NewReader(cancelPayloadWrong))
	recCancelWrong := httptest.NewRecorder()
	HandleBillByID(recCancelWrong, reqCancelWrong)
	if recCancelWrong.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for cancel with wrong PIN, got %d", recCancelWrong.Code)
	}

	// 5. Test POST /api/bills/{id}/cancel with valid Master PIN ("1234")
	cancelPayloadRight, _ := json.Marshal(map[string]string{"pin": "1234"})
	reqCancelRight := httptest.NewRequest("POST", "/api/bills/"+createdBill.ID+"/cancel", bytes.NewReader(cancelPayloadRight))
	recCancelRight := httptest.NewRecorder()
	HandleBillByID(recCancelRight, reqCancelRight)
	if recCancelRight.Code != http.StatusOK {
		t.Fatalf("Expected 200 for cancel with right PIN, got %d: %s", recCancelRight.Code, recCancelRight.Body.String())
	}

	// Stock should be restored to 50
	pCheck, _ = db.GetProductByID(p.ID)
	if pCheck.Stock != 50.0 {
		t.Errorf("Expected stock restored to 50.0, got %.2f", pCheck.Stock)
	}
}

func TestHandleReportsAndCustomers(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	// Save customer
	_ = db.SaveCustomer(db.Customer{
		Name:  "Sundaram",
		Phone: "9123456780",
		State: "Tamil Nadu",
	})

	// Test GET /api/customers?q=Sundaram
	reqCust := httptest.NewRequest("GET", "/api/customers?q=Sundaram", nil)
	recCust := httptest.NewRecorder()
	HandleCustomers(recCust, reqCust)

	if recCust.Code != http.StatusOK {
		t.Fatalf("Expected 200 for customer search, got %d", recCust.Code)
	}

	var custList []db.Customer
	_ = json.NewDecoder(recCust.Body).Decode(&custList)
	if len(custList) == 0 || custList[0].Name != "Sundaram" {
		t.Errorf("Expected customer 'Sundaram' found, got %v", custList)
	}

	// Test GET /api/reports/gstr1
	reqGSTR := httptest.NewRequest("GET", "/api/reports/gstr1?from=2026-10-01&to=2026-10-31", nil)
	recGSTR := httptest.NewRecorder()
	HandleGSTR1Report(recGSTR, reqGSTR)

	if recGSTR.Code != http.StatusOK {
		t.Fatalf("Expected 200 for GSTR-1 report, got %d", recGSTR.Code)
	}
}

func TestHandleBackupAndRestore(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	// Export backup
	reqBackup := httptest.NewRequest("GET", "/api/backup", nil)
	recBackup := httptest.NewRecorder()
	HandleBackup(recBackup, reqBackup)

	if recBackup.Code != http.StatusOK {
		t.Fatalf("Expected 200 for backup, got %d", recBackup.Code)
	}

	var backupData BackupData
	if err := json.NewDecoder(recBackup.Body).Decode(&backupData); err != nil {
		t.Fatalf("Failed to decode backup JSON: %v", err)
	}

	// Restore backup
	bodyRestore, _ := json.Marshal(backupData)
	reqRestore := httptest.NewRequest("POST", "/api/restore", bytes.NewReader(bodyRestore))
	recRestore := httptest.NewRecorder()
	HandleRestore(recRestore, reqRestore)

	if recRestore.Code != http.StatusOK {
		t.Fatalf("Expected 200 for restore, got %d: %s", recRestore.Code, recRestore.Body.String())
	}
}

func TestSplitPaymentBill(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	// Create test product with stock
	prod := db.Product{
		ID:       "PROD_SPLIT_01",
		NameEn:   "Groundnut Oil 1L",
		Price:    350.0,
		Stock:    10,
		Category: "Oil",
		GSTPct:   5.0,
	}
	if err := db.SaveProduct(prod); err != nil {
		t.Fatalf("Failed to save product: %v", err)
	}

	// Create Split Bill (2 x 350 = 700: 200 Cash + 500 UPI)
	createReq := map[string]interface{}{
		"date": "2026-10-28",
		"customer": map[string]string{
			"name":  "Sundar",
			"phone": "9876543210",
			"state": "Tamil Nadu",
		},
		"items": []map[string]interface{}{
			{
				"productId": prod.ID,
				"nameEn":    prod.NameEn,
				"qty":       2,
				"price":     350.0,
				"gstPct":    5.0,
			},
		},
		"discount": 0.0,
		"gstOn":    true,
		"payMode":  "Split",
		"paid":     700.0,
		"cashPaid": 200.0,
		"upiPaid":  500.0,
	}

	body, _ := json.Marshal(createReq)
	req := httptest.NewRequest("POST", "/api/bills", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	HandleBills(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var created db.Bill
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if created.PayMode != "Split" {
		t.Errorf("Expected PayMode 'Split', got '%s'", created.PayMode)
	}
	if created.CashPaid != 200.0 {
		t.Errorf("Expected CashPaid 200.0, got %.2f", created.CashPaid)
	}
	if created.UPIPaid != 500.0 {
		t.Errorf("Expected UPIPaid 500.0, got %.2f", created.UPIPaid)
	}
	if created.Paid != 700.0 {
		t.Errorf("Expected Paid 700.0, got %.2f", created.Paid)
	}
	if created.Balance != 0.0 {
		t.Errorf("Expected Balance 0.0, got %.2f", created.Balance)
	}

	// Fetch via GET /api/bills/{id}
	reqGet := httptest.NewRequest("GET", "/api/bills/"+created.ID, nil)
	recGet := httptest.NewRecorder()
	HandleBillByID(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET bill, got %d: %s", recGet.Code, recGet.Body.String())
	}

	var fetched db.Bill
	if err := json.NewDecoder(recGet.Body).Decode(&fetched); err != nil {
		t.Fatalf("Failed to decode fetched bill: %v", err)
	}

	if fetched.PayMode != "Split" || fetched.CashPaid != 200.0 || fetched.UPIPaid != 500.0 {
		t.Errorf("Fetched bill mismatch: PayMode=%s, CashPaid=%.2f, UPIPaid=%.2f",
			fetched.PayMode, fetched.CashPaid, fetched.UPIPaid)
	}
}

func TestCategoriesAndCustomerCRM(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	// 1. Categories: GET & POST
	reqGetCat := httptest.NewRequest("GET", "/api/categories", nil)
	recGetCat := httptest.NewRecorder()
	HandleCategories(recGetCat, reqGetCat)
	if recGetCat.Code != http.StatusOK {
		t.Fatalf("Expected 200 for GET categories, got %d", recGetCat.Code)
	}

	// Add custom category
	catPayload := `{"name":"Millets","nameTa":"தானியங்கள்"}`
	reqPostCat := httptest.NewRequest("POST", "/api/categories", strings.NewReader(catPayload))
	recPostCat := httptest.NewRecorder()
	HandleCategories(recPostCat, reqPostCat)
	if recPostCat.Code != http.StatusOK {
		t.Fatalf("Expected 200 for POST category, got %d", recPostCat.Code)
	}

	// 2. Customer Lookup: First-time phone should be Unregistered
	reqLookup1 := httptest.NewRequest("GET", "/api/customers/lookup?phone=9876543210", nil)
	recLookup1 := httptest.NewRecorder()
	HandleCustomerLookup(recLookup1, reqLookup1)
	if recLookup1.Code != http.StatusOK {
		t.Fatalf("Expected 200 for customer lookup, got %d", recLookup1.Code)
	}
	var res1 db.CustomerLookupResult
	_ = json.NewDecoder(recLookup1.Body).Decode(&res1)
	if res1.IsRegistered {
		t.Errorf("Expected first-time customer to be IsRegistered=false, got true")
	}

	// 3. Create a bill for this customer
	billPayload := `{
		"date": "2026-10-28",
		"customer": {
			"name": "Karthik Raja",
			"phone": "9876543210",
			"address": "Villupuram",
			"state": "Tamil Nadu"
		},
		"items": [
			{
				"productId": "gnut1000",
				"nameEn": "Groundnut Oil 1L",
				"nameTa": "நிலக்கடலை எண்ணெய் 1லி",
				"hsn": "1512",
				"qty": 3,
				"price": 210,
				"gstPct": 5
			}
		],
		"payMode": "Cash",
		"paid": 630,
		"gstOn": false
	}`
	reqBill := httptest.NewRequest("POST", "/api/bills", strings.NewReader(billPayload))
	recBill := httptest.NewRecorder()
	HandleBills(recBill, reqBill)
	if recBill.Code != http.StatusOK && recBill.Code != http.StatusCreated {
		t.Fatalf("Expected 200/201 for bill creation, got %d: %s", recBill.Code, recBill.Body.String())
	}

	// 4. Customer Lookup again: Should now be Registered!
	reqLookup2 := httptest.NewRequest("GET", "/api/customers/lookup?phone=9876543210", nil)
	recLookup2 := httptest.NewRecorder()
	HandleCustomerLookup(recLookup2, reqLookup2)
	var res2 db.CustomerLookupResult
	_ = json.NewDecoder(recLookup2.Body).Decode(&res2)
	if !res2.IsRegistered {
		t.Errorf("Expected returning customer to be IsRegistered=true after bill, got false")
	}
	if res2.TotalBillsCount != 1 || res2.TotalSpent != 630.0 {
		t.Errorf("Expected 1 bill and ₹630 spend, got %d bills, ₹%.2f", res2.TotalBillsCount, res2.TotalSpent)
	}

	// 5. Customer History
	reqHist := httptest.NewRequest("GET", "/api/customers/history?q=9876543210", nil)
	recHist := httptest.NewRecorder()
	HandleCustomerHistory(recHist, reqHist)
	if recHist.Code != http.StatusOK {
		t.Fatalf("Expected 200 for customer history, got %d", recHist.Code)
	}
	var hist db.CustomerHistory
	_ = json.NewDecoder(recHist.Body).Decode(&hist)
	if len(hist.ProductSummary) == 0 || hist.ProductSummary[0].NameEn != "Groundnut Oil 1L" || hist.ProductSummary[0].TotalQty != 3 {
		t.Errorf("Customer product purchase history mismatch: %v", hist.ProductSummary)
	}

	// 6. Customer Directory
	reqDir := httptest.NewRequest("GET", "/api/customers/directory?q=Karthik", nil)
	recDir := httptest.NewRecorder()
	HandleCustomerDirectory(recDir, reqDir)
	if recDir.Code != http.StatusOK {
		t.Fatalf("Expected 200 for customer directory, got %d", recDir.Code)
	}
	var dir []db.CustomerDirectoryItem
	_ = json.NewDecoder(recDir.Body).Decode(&dir)
	if len(dir) == 0 || dir[0].Name != "Karthik Raja" {
		t.Errorf("Expected Karthik Raja in directory, got %v", dir)
	}

	// 7. Verify GSTR-1 inclusion of Non-GST bill
	reqGSTR := httptest.NewRequest("GET", "/api/reports/gstr1?from=2026-10-01&to=2026-10-31", nil)
	recGSTR := httptest.NewRecorder()
	HandleGSTR1Report(recGSTR, reqGSTR)
	if recGSTR.Code != http.StatusOK {
		t.Fatalf("Expected 200 for GSTR-1 report, got %d", recGSTR.Code)
	}

	var gstr struct {
		GrandTotal   float64 `json:"grandTotal"`
		TotalTaxable float64 `json:"totalTaxable"`
		TotalTax     float64 `json:"totalTax"`
		HSNSummary   []struct {
			HSN string `json:"hsn"`
		} `json:"hsnSummary"`
	}
	_ = json.NewDecoder(recGSTR.Body).Decode(&gstr)

	if gstr.GrandTotal != 630.0 {
		t.Errorf("Expected Non-GST bill included in GSTR-1 GrandTotal 630.0, got %.2f", gstr.GrandTotal)
	}
	if gstr.TotalTaxable <= 0 || gstr.TotalTax <= 0 {
		t.Errorf("Expected statutory tax computed for Non-GST bill, got taxable=%.2f, tax=%.2f", gstr.TotalTaxable, gstr.TotalTax)
	}
	if len(gstr.HSNSummary) == 0 {
		t.Errorf("Expected HSN summary populated for Non-GST bill, got empty")
	}

	// 8. Test Category Edit & Rename with Product cascade
	cats, _ := db.GetCategories()
	var spicesCat db.Category
	for _, c := range cats {
		if c.Name == "Spices / மசாலா" {
			spicesCat = c
			break
		}
	}
	if spicesCat.ID != "" {
		spicesCat.Name = "Organic Spices"
		spicesCat.NameTa = "இயற்கை மசாலா"
		updatePayload, _ := json.Marshal(spicesCat)
		reqUpCat := httptest.NewRequest("POST", "/api/categories", bytes.NewReader(updatePayload))
		recUpCat := httptest.NewRecorder()
		HandleCategories(recUpCat, reqUpCat)
		if recUpCat.Code != http.StatusOK {
			t.Fatalf("Expected 200 for Category update, got %d", recUpCat.Code)
		}
	}

	// 9. Test Customer Delete via HandleCustomerByID
	reqDelCust := httptest.NewRequest("DELETE", "/api/customers/9876543210", nil)
	recDelCust := httptest.NewRecorder()
	HandleCustomerByID(recDelCust, reqDelCust)
	if recDelCust.Code != http.StatusOK {
		t.Fatalf("Expected 200 for customer delete, got %d", recDelCust.Code)
	}
}

func TestZeroGSTExemptSeeds(t *testing.T) {
	// Raw agricultural seeds and millets are 0% GST (Exempt)
	items := []db.BillItem{
		{
			ProductID: "seeds1",
			NameEn:    "Sesame Seeds 1kg",
			Qty:       2,
			Price:     150.0, // Line amount = 300
			GSTPct:    0.0,   // 0% GST Exempt
		},
	}

	subtotal, taxable, gstAmt, cgst, sgst, igst, grandTotal, processed := ComputeBill(items, 0, true, "Tamil Nadu", "Tamil Nadu")

	if grandTotal != 300.0 {
		t.Errorf("Expected GrandTotal 300.0 for 0%% GST seeds, got %.2f", grandTotal)
	}
	if subtotal != 300.0 {
		t.Errorf("Expected Subtotal 300.0, got %.2f", subtotal)
	}
	if gstAmt != 0.0 {
		t.Errorf("Expected 0 GST for exempt seeds, got %.2f", gstAmt)
	}
	if cgst != 0.0 || sgst != 0.0 || igst != 0.0 {
		t.Errorf("Expected 0 CGST/SGST/IGST, got CGST=%.2f, SGST=%.2f, IGST=%.2f", cgst, sgst, igst)
	}
	if taxable != 300.0 {
		t.Errorf("Expected Taxable value 300.0, got %.2f", taxable)
	}
	if len(processed) > 0 && processed[0].GSTPortion != 0.0 {
		t.Errorf("Expected 0 GST portion on seed item, got %.2f", processed[0].GSTPortion)
	}
}

func TestCustomerLedgerAndPayments(t *testing.T) {
	tmpDir := setupTestDBForHandlers(t)
	defer os.RemoveAll(tmpDir)

	prods, err := db.GetProducts()
	if err != nil || len(prods) == 0 {
		t.Fatalf("Expected seeded products, got: %v", err)
	}
	p := prods[0]
	// Give ample stock
	p.Stock = 500
	_ = db.SaveProduct(p)

	phone := "9842112233"
	custName := "Vanur Farmer Producer Co"

	// 1. Create a credit sale bill for 21,000 (Paid = 0)
	bill1 := db.Bill{
		BillNo: "VAM-2026-001",
		Date:   "2026-10-05",
		Customer: db.Customer{
			Name:  custName,
			Phone: phone,
		},
		Items: []db.BillItem{
			{ProductID: p.ID, NameEn: p.NameEn, Qty: 100, Price: 210.0, Amount: 21000.0},
		},
		Subtotal:   21000.0,
		GrandTotal: 21000.0,
		PayMode:    "Credit",
		Paid:       0,
		Balance:    21000.0,
	}
	_, err = db.CreateBill(bill1)
	if err != nil {
		t.Fatalf("Failed to create bill1: %v", err)
	}

	// 2. Create another bill for 8,400 (Paid = 0) on 2026-10-10
	bill2 := db.Bill{
		BillNo: "VAM-2026-002",
		Date:   "2026-10-10",
		Customer: db.Customer{
			Name:  custName,
			Phone: phone,
		},
		Items: []db.BillItem{
			{ProductID: p.ID, NameEn: p.NameEn, Qty: 40, Price: 210.0, Amount: 8400.0},
		},
		Subtotal:   8400.0,
		GrandTotal: 8400.0,
		PayMode:    "Credit",
		Paid:       0,
		Balance:    8400.0,
	}
	_, err = db.CreateBill(bill2)
	if err != nil {
		t.Fatalf("Failed to create bill2: %v", err)
	}

	// 3. Post a bank payment receipt via HandleCustomerPayments
	paymentPayload := db.CustomerPayment{
		Date:          "2026-10-15",
		CustomerName:  custName,
		CustomerPhone: phone,
		Amount:        20000.0,
		PayMode:       "Bank Transfer",
		BankName:      "The Karur Vysya Bank",
		RefNo:         "UTR99281726",
		Note:          "October Advance RTGS",
	}
	body, _ := json.Marshal(paymentPayload)
	reqPay := httptest.NewRequest("POST", "/api/customers/payments", bytes.NewReader(body))
	recPay := httptest.NewRecorder()
	HandleCustomerPayments(recPay, reqPay)
	if recPay.Code != http.StatusOK {
		t.Fatalf("Expected 200 for payment record, got %d: %s", recPay.Code, recPay.Body.String())
	}

	var savedPayment db.CustomerPayment
	_ = json.NewDecoder(recPay.Body).Decode(&savedPayment)
	if savedPayment.ID == "" || savedPayment.Amount != 20000.0 {
		t.Fatalf("Saved payment mismatch: %v", savedPayment)
	}

	// 4. Test Customer Ledger API: /api/customers/ledger?q=9842112233
	reqLedger := httptest.NewRequest("GET", "/api/customers/ledger?q="+phone, nil)
	recLedger := httptest.NewRecorder()
	HandleCustomerLedger(recLedger, reqLedger)
	if recLedger.Code != http.StatusOK {
		t.Fatalf("Expected 200 for customer ledger, got %d: %s", recLedger.Code, recLedger.Body.String())
	}

	var ledger db.CustomerLedger
	_ = json.NewDecoder(recLedger.Body).Decode(&ledger)

	// Verify entries and calculations:
	// Bill 1: Debit 21000, Credit 0 -> Balance 21000
	// Bill 2: Debit 8400, Credit 0 -> Balance 29400
	// Receipt: Debit 0, Credit 20000 -> Balance 9400
	if len(ledger.Entries) != 3 {
		t.Fatalf("Expected 3 ledger entries, got %d", len(ledger.Entries))
	}
	if ledger.TotalDebit != 29400.0 {
		t.Errorf("Expected TotalDebit 29400, got %.2f", ledger.TotalDebit)
	}
	if ledger.TotalCredit != 20000.0 {
		t.Errorf("Expected TotalCredit 20000, got %.2f", ledger.TotalCredit)
	}
	if ledger.ClosingBalance != 9400.0 {
		t.Errorf("Expected ClosingBalance 9400, got %.2f", ledger.ClosingBalance)
	}
	if ledger.BalanceType != "Debit Balance" {
		t.Errorf("Expected 'Debit Balance', got '%s'", ledger.BalanceType)
	}

	// Verify running balance of 3rd entry is 9400
	if ledger.Entries[2].Balance != 9400.0 {
		t.Errorf("Expected running balance 9400, got %.2f", ledger.Entries[2].Balance)
	}

	// 5. Test Date Filter & Opening Balance
	// From: 2026-10-12 to 2026-10-31
	// Prior events: Bill 1 (21000) and Bill 2 (8400) -> Prior Debits = 29400
	// Opening Balance should be 29400
	// In-period: Payment (Credit 20000)
	// Closing balance should be 29400 - 20000 = 9400
	reqPeriod := httptest.NewRequest("GET", "/api/customers/ledger?q="+phone+"&from=2026-10-12&to=2026-10-31", nil)
	recPeriod := httptest.NewRecorder()
	HandleCustomerLedger(recPeriod, reqPeriod)
	var periodLedger db.CustomerLedger
	_ = json.NewDecoder(recPeriod.Body).Decode(&periodLedger)

	if periodLedger.OpeningBalance != 29400.0 {
		t.Errorf("Expected OpeningBalance 29400, got %.2f", periodLedger.OpeningBalance)
	}
	if len(periodLedger.Entries) != 1 {
		t.Errorf("Expected 1 entry in filtered period, got %d", len(periodLedger.Entries))
	}
	if periodLedger.ClosingBalance != 9400.0 {
		t.Errorf("Expected ClosingBalance 9400, got %.2f", periodLedger.ClosingBalance)
	}

	// 6. Delete Payment Receipt
	reqDel := httptest.NewRequest("DELETE", "/api/customers/payments/"+savedPayment.ID, nil)
	recDel := httptest.NewRecorder()
	HandleCustomerPaymentDelete(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("Expected 200 for payment delete, got %d", recDel.Code)
	}

	// Check ledger again: TotalCredit should be 0, ClosingBalance should be 29400
	recLedger2 := httptest.NewRecorder()
	HandleCustomerLedger(recLedger2, reqLedger)
	var ledger2 db.CustomerLedger
	_ = json.NewDecoder(recLedger2.Body).Decode(&ledger2)
	if ledger2.TotalCredit != 0.0 || ledger2.ClosingBalance != 29400.0 {
		t.Errorf("Expected 0 credit after delete, got TotalCredit=%.2f, ClosingBalance=%.2f", ledger2.TotalCredit, ledger2.ClosingBalance)
	}
}


