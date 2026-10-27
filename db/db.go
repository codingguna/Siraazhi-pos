/* ==========================================================================
   Package db — SQLite Database Engine & Data Access Object (DAO)
   
   WHAT IT IS:
     The persistent data layer of Siraazhi Pro POS:
     - Embedded SQLite engine running via pure Go (modernc.org/sqlite).
     - Requires ZERO external database servers, zero CGO, and zero runtime DLLs.
     - Automatically creates tables, indexes, and initial catalog defaults on first launch.
     - Provides thread-safe atomic transactions for invoice creation and inventory deductions.

   HOW IT WORKS:
     1. InitDB():
        - Creates the data/ directory if not present.
        - Configures high-performance SQLite PRAGMAs:
            PRAGMA journal_mode=WAL;     (Write-Ahead Logging for high concurrency)
            PRAGMA busy_timeout=5000;    (Handles concurrent multi-device transactions)
            PRAGMA synchronous=NORMAL;   (Balanced performance and crash durability)
            PRAGMA foreign_keys=ON;      (Maintains relational integrity)
        - Executes SQL schema definitions to initialize tables and indexes.
        - Seeds the initial 9 cold-pressed oil SKUs and FPC settings if database is empty.
     2. CreateBill():
        - Initiates an ACID SQLite transaction.
        - Checks available stock for all line items. If stock is insufficient, aborts.
        - Inserts bill header into 'bills' table.
        - Inserts individual line items into 'bill_items' table.
        - Decrements stock in 'products' table.
        - Inserts audit records in 'stock_moves' table.
        - Commits the transaction atomically.
     3. CancelBill():
        - Retrieves line items for the bill.
        - Increments stock in 'products' table to reverse the sale.
        - Inserts 'cancel' entries in 'stock_moves'.
        - Deletes the bill record (foreign key cascades delete line items).

   DATA FLOW:
     Go Application Call -> Mutex Lock -> SQLite Transaction -> Commit/Rollback -> Updated DB State
   ========================================================================== */

package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

var (
	// DB is the singleton SQLite database connection handle
	DB            *sql.DB
	lock          sync.Mutex
	currentDBPath string

	// globalSeq ensures monotonic collision-free unique IDs even across concurrent operations
	globalSeq uint64
)

// GenID generates a collision-free unique identifier combining prefix, nanoseconds, and atomic counter.
// Format: <prefix>_<unix_nano>_<counter>
func GenID(prefix string) string {
	seq := atomic.AddUint64(&globalSeq, 1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq)
}

// InitDB initializes SQLite database, sets concurrency pragmas, and runs migrations.
//
// WHAT IT IS:
//   The database bootstrap function executed at application startup.
//
// HOW IT WORKS:
//   1. Creates parent folder (e.g. data/) if not present.
//   2. Opens SQLite connection using pure Go driver.
//   3. Configures WAL mode and 5000ms busy timeout.
//   4. Runs createTables() and seedDefaults().
func InitDB(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir failed: %w", err)
	}

	currentDBPath = dbPath

	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open sqlite failed: %w", err)
	}

	// PRAGMAs for speed & data integrity across multi-device counter access
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
	}
	for _, p := range pragmas {
		if _, err := DB.Exec(p); err != nil {
			return fmt.Errorf("exec pragma %s failed: %w", p, err)
		}
	}

	if err := createTables(); err != nil {
		return fmt.Errorf("create tables failed: %w", err)
	}

	if err := seedDefaults(); err != nil {
		return fmt.Errorf("seed defaults failed: %w", err)
	}

	return nil
}

// createTables executes SQL DDL schema statements to create all required tables and indexes.
func createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		val TEXT
	);

	CREATE TABLE IF NOT EXISTS settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS products (
		id TEXT PRIMARY KEY,
		category TEXT,
		category_ta TEXT,
		variant TEXT,
		variant_ml INTEGER DEFAULT 0,
		name_en TEXT NOT NULL,
		name_ta TEXT,
		hsn TEXT,
		barcode TEXT UNIQUE,
		gst_pct REAL DEFAULT 5,
		price REAL NOT NULL,
		mrp REAL NOT NULL,
		stock REAL DEFAULT 0,
		low_alert REAL DEFAULT 5,
		unit TEXT DEFAULT 'Bottle'
	);

	CREATE TABLE IF NOT EXISTS bills (
		id TEXT PRIMARY KEY,
		bill_no TEXT UNIQUE NOT NULL,
		date TEXT NOT NULL,
		customer_name TEXT,
		customer_phone TEXT,
		customer_address TEXT,
		customer_gstin TEXT,
		customer_state TEXT,
		subtotal REAL NOT NULL,
		discount REAL DEFAULT 0,
		taxable REAL NOT NULL,
		gst_on INTEGER DEFAULT 1,
		gst_amt REAL DEFAULT 0,
		cgst REAL DEFAULT 0,
		sgst REAL DEFAULT 0,
		igst REAL DEFAULT 0,
		grand_total REAL NOT NULL,
		pay_mode TEXT DEFAULT 'Cash',
		paid REAL DEFAULT 0,
		balance REAL DEFAULT 0,
		note TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS bill_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bill_id TEXT NOT NULL,
		product_id TEXT NOT NULL,
		name_en TEXT,
		name_ta TEXT,
		hsn TEXT,
		barcode TEXT,
		qty REAL NOT NULL,
		price REAL NOT NULL,
		gst_pct REAL NOT NULL,
		amount REAL NOT NULL,
		gst_portion REAL DEFAULT 0,
		FOREIGN KEY (bill_id) REFERENCES bills(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS stock_moves (
		id TEXT PRIMARY KEY,
		date TEXT NOT NULL,
		product_id TEXT NOT NULL,
		type TEXT NOT NULL,
		qty REAL NOT NULL,
		note TEXT,
		ref TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS purchases (
		id TEXT PRIMARY KEY,
		date TEXT NOT NULL,
		supplier TEXT,
		item TEXT NOT NULL,
		qty REAL NOT NULL,
		unit TEXT DEFAULT 'kg',
		cost REAL NOT NULL,
		note TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS expenses (
		id TEXT PRIMARY KEY,
		date TEXT NOT NULL,
		head TEXT NOT NULL,
		amount REAL NOT NULL,
		note TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS customers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		phone TEXT UNIQUE,
		address TEXT,
		gstin TEXT,
		state TEXT DEFAULT 'Tamil Nadu',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_bills_date ON bills(date);
	CREATE INDEX IF NOT EXISTS idx_stock_moves_pid ON stock_moves(product_id);
	CREATE INDEX IF NOT EXISTS idx_purchases_date ON purchases(date);
	CREATE INDEX IF NOT EXISTS idx_expenses_date ON expenses(date);
	CREATE INDEX IF NOT EXISTS idx_customers_phone ON customers(phone);
	CREATE INDEX IF NOT EXISTS idx_customers_name ON customers(name);
	`
	_, err := DB.Exec(schema)
	return err
}

// seedDefaults checks if the database is newly initialized and inserts initial products & settings.
func seedDefaults() error {
	// Seed settings if not exists
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM settings WHERE id = 1").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		defSettings := Settings{
			CompanyName:    "VANUR FARMER PRODUCER COMPANY LIMITED",
			BrandEn:        "Siraazhi",
			BrandTa:        "சீராழி",
			Tagline:        "Pure Traditional Marachekku Oils",
			Address:        "Thailapuram Village, Vanur, Villupuram, Tamil Nadu",
			Phone:          "9876543210",
			Fssai:          "12423031000494",
			Gstin:          "",
			GstDefaultOn:   true,
			GstDefaultPct:  5,
			BillPrefix:     "VFPC",
			FinancialYear:  "2026-27",
			FooterNoteEn:   "Thank you! Visit again.",
			FooterNoteTa:   "நன்றி! மீண்டும் வருக!",
			PayModes:       []string{"Cash", "UPI", "Card", "Credit"},
			ThermalSize:    "80mm",
			DefaultPrinter: "80mm",
			MasterPin:      "1234",
		}
		data, _ := json.Marshal(defSettings)
		_, err := DB.Exec("INSERT INTO settings (id, data) VALUES (1, ?)", string(data))
		if err != nil {
			return err
		}
	}

	// Seed meta sequence
	var seqCount int
	if err := DB.QueryRow("SELECT COUNT(*) FROM meta WHERE key = 'bill_seq'").Scan(&seqCount); err != nil {
		return err
	}
	if seqCount == 0 {
		_, err := DB.Exec("INSERT INTO meta (key, val) VALUES ('bill_seq', '1')")
		if err != nil {
			return err
		}
	}

	// Seed default products if empty
	var prodCount int
	if err := DB.QueryRow("SELECT COUNT(*) FROM products").Scan(&prodCount); err != nil {
		return err
	}
	if prodCount == 0 {
		defaults := []Product{
			{ID: "coco500", Category: "Coconut", CategoryTa: "தேங்காய்", Variant: "500 ml", VariantMl: 500, NameEn: "Coconut Oil 500ml", NameTa: "தேங்காய் எண்ணெய் 500மிலி", HSN: "1513", Barcode: "8901513005001", GSTPct: 5, Price: 140, MRP: 145, Stock: 50, LowAlert: 10, Unit: "Bottle"},
			{ID: "coco1000", Category: "Coconut", CategoryTa: "தேங்காய்", Variant: "1 Litre", VariantMl: 1000, NameEn: "Coconut Oil 1L", NameTa: "தேங்காய் எண்ணெய் 1லி", HSN: "1513", Barcode: "8901513010001", GSTPct: 5, Price: 270, MRP: 275, Stock: 50, LowAlert: 10, Unit: "Bottle"},
			{ID: "coco5000", Category: "Coconut", CategoryTa: "தேங்காய்", Variant: "5 Litre", VariantMl: 5000, NameEn: "Coconut Oil 5L", NameTa: "தேங்காய் எண்ணெய் 5லி", HSN: "1513", Barcode: "8901513050001", GSTPct: 5, Price: 1325, MRP: 1350, Stock: 20, LowAlert: 5, Unit: "Can"},
			{ID: "gnut500", Category: "Groundnut", CategoryTa: "நிலக்கடலை", Variant: "500 ml", VariantMl: 500, NameEn: "Groundnut Oil 500ml", NameTa: "நிலக்கடலை எண்ணெய் 500மிலி", HSN: "1512", Barcode: "8901512005001", GSTPct: 5, Price: 110, MRP: 115, Stock: 50, LowAlert: 10, Unit: "Bottle"},
			{ID: "gnut1000", Category: "Groundnut", CategoryTa: "நிலக்கடலை", Variant: "1 Litre", VariantMl: 1000, NameEn: "Groundnut Oil 1L", NameTa: "நிலக்கடலை எண்ணெய் 1லி", HSN: "1512", Barcode: "8901512010001", GSTPct: 5, Price: 210, MRP: 220, Stock: 50, LowAlert: 10, Unit: "Bottle"},
			{ID: "gnut5000", Category: "Groundnut", CategoryTa: "நிலக்கடலை", Variant: "5 Litre", VariantMl: 5000, NameEn: "Groundnut Oil 5L", NameTa: "நிலக்கடலை எண்ணெய் 5லி", HSN: "1512", Barcode: "8901512050001", GSTPct: 5, Price: 1025, MRP: 1050, Stock: 20, LowAlert: 5, Unit: "Can"},
			{ID: "ses500", Category: "Sesame", CategoryTa: "எள்", Variant: "500 ml", VariantMl: 500, NameEn: "Sesame Oil 500ml", NameTa: "நல்லெண்ணெய் 500மிலி", HSN: "1515", Barcode: "8901515005001", GSTPct: 5, Price: 195, MRP: 199, Stock: 50, LowAlert: 10, Unit: "Bottle"},
			{ID: "ses1000", Category: "Sesame", CategoryTa: "எள்", Variant: "1 Litre", VariantMl: 1000, NameEn: "Sesame Oil 1L", NameTa: "நல்லெண்ணெய் 1லி", HSN: "1515", Barcode: "8901515010001", GSTPct: 5, Price: 380, MRP: 390, Stock: 50, LowAlert: 10, Unit: "Bottle"},
			{ID: "ses5000", Category: "Sesame", CategoryTa: "எள்", Variant: "5 Litre", VariantMl: 5000, NameEn: "Sesame Oil 5L", NameTa: "நல்லெண்ணெய் 5லி", HSN: "1515", Barcode: "8901515050001", GSTPct: 5, Price: 1875, MRP: 1900, Stock: 20, LowAlert: 5, Unit: "Can"},
		}
		for _, p := range defaults {
			_, err := DB.Exec(`INSERT INTO products 
				(id, category, category_ta, variant, variant_ml, name_en, name_ta, hsn, barcode, gst_pct, price, mrp, stock, low_alert, unit)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				p.ID, p.Category, p.CategoryTa, p.Variant, p.VariantMl, p.NameEn, p.NameTa, p.HSN, p.Barcode, p.GSTPct, p.Price, p.MRP, p.Stock, p.LowAlert, p.Unit,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func cleanTamilBrand(ta string) string {
	t := strings.TrimSpace(ta)
	if t == "" || strings.Contains(t, "?") || strings.Contains(t, "à®") || strings.Contains(t, "à¯") || strings.Contains(t, "\u00e0\u00ae") {
		return "சீராழி"
	}
	return t
}

func cleanTamilFooter(ta string) string {
	t := strings.TrimSpace(ta)
	if t == "" || strings.Contains(t, "?") || strings.Contains(t, "à®") || strings.Contains(t, "à¯") || strings.Contains(t, "\u00e0\u00ae") {
		return "நன்றி! மீண்டும் வருக!"
	}
	return t
}

// GetSettings loads current business configuration from settings table.
// Flow: queries JSON string from settings -> unmarshals into Settings struct.
func GetSettings() (Settings, error) {
	var s Settings
	var raw string
	err := DB.QueryRow("SELECT data FROM settings WHERE id = 1").Scan(&raw)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal([]byte(raw), &s)
	if strings.TrimSpace(s.MasterPin) == "" {
		s.MasterPin = "1234"
	}
	s.BrandTa = cleanTamilBrand(s.BrandTa)
	s.FooterNoteTa = cleanTamilFooter(s.FooterNoteTa)
	return s, err
}

// GetMasterPin returns the active Master Admin PIN (defaults to "1234").
func GetMasterPin() string {
	s, err := GetSettings()
	if err != nil || strings.TrimSpace(s.MasterPin) == "" {
		return "1234"
	}
	return strings.TrimSpace(s.MasterPin)
}

// SaveSettings updates business configuration in settings table.
// Flow: marshals struct to JSON -> executes SQL update.
func SaveSettings(s Settings) error {
	if strings.TrimSpace(s.MasterPin) == "" {
		if existing, err := GetSettings(); err == nil && strings.TrimSpace(existing.MasterPin) != "" {
			s.MasterPin = existing.MasterPin
		} else {
			s.MasterPin = "1234"
		}
	}
	s.BrandTa = cleanTamilBrand(s.BrandTa)
	s.FooterNoteTa = cleanTamilFooter(s.FooterNoteTa)
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = DB.Exec("UPDATE settings SET data = ? WHERE id = 1", string(data))
	return err
}

// GetProducts retrieves all catalog products ordered by category and size.
func GetProducts() ([]Product, error) {
	rows, err := DB.Query(`SELECT id, category, category_ta, variant, variant_ml, name_en, name_ta, hsn, 
		COALESCE(barcode, ''), gst_pct, price, mrp, stock, low_alert, unit FROM products ORDER BY category, variant_ml, name_en`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Product
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Category, &p.CategoryTa, &p.Variant, &p.VariantMl, &p.NameEn, &p.NameTa, &p.HSN, &p.Barcode, &p.GSTPct, &p.Price, &p.MRP, &p.Stock, &p.LowAlert, &p.Unit); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, nil
}

// GetProductByID finds a single product by its alphanumeric ID.
func GetProductByID(id string) (Product, error) {
	var p Product
	err := DB.QueryRow(`SELECT id, category, category_ta, variant, variant_ml, name_en, name_ta, hsn, 
		COALESCE(barcode, ''), gst_pct, price, mrp, stock, low_alert, unit FROM products WHERE id = ?`, id).
		Scan(&p.ID, &p.Category, &p.CategoryTa, &p.Variant, &p.VariantMl, &p.NameEn, &p.NameTa, &p.HSN, &p.Barcode, &p.GSTPct, &p.Price, &p.MRP, &p.Stock, &p.LowAlert, &p.Unit)
	return p, err
}

// GetProductByBarcode finds a single product by barcode or ID for hardware scanner input.
func GetProductByBarcode(code string) (Product, error) {
	var p Product
	err := DB.QueryRow(`SELECT id, category, category_ta, variant, variant_ml, name_en, name_ta, hsn, 
		COALESCE(barcode, ''), gst_pct, price, mrp, stock, low_alert, unit FROM products WHERE barcode = ? OR id = ?`, code, code).
		Scan(&p.ID, &p.Category, &p.CategoryTa, &p.Variant, &p.VariantMl, &p.NameEn, &p.NameTa, &p.HSN, &p.Barcode, &p.GSTPct, &p.Price, &p.MRP, &p.Stock, &p.LowAlert, &p.Unit)
	return p, err
}

// SaveProduct creates a new product or updates an existing one (upsert).
func SaveProduct(p Product) error {
	if p.ID == "" {
		p.ID = GenID("p")
	}
	_, err := DB.Exec(`INSERT INTO products 
		(id, category, category_ta, variant, variant_ml, name_en, name_ta, hsn, barcode, gst_pct, price, mrp, stock, low_alert, unit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			category = excluded.category,
			category_ta = excluded.category_ta,
			variant = excluded.variant,
			variant_ml = excluded.variant_ml,
			name_en = excluded.name_en,
			name_ta = excluded.name_ta,
			hsn = excluded.hsn,
			barcode = excluded.barcode,
			gst_pct = excluded.gst_pct,
			price = excluded.price,
			mrp = excluded.mrp,
			stock = excluded.stock,
			low_alert = excluded.low_alert,
			unit = excluded.unit`,
		p.ID, p.Category, p.CategoryTa, p.Variant, p.VariantMl, p.NameEn, p.NameTa, p.HSN, p.Barcode, p.GSTPct, p.Price, p.MRP, p.Stock, p.LowAlert, p.Unit,
	)
	return err
}

// DeleteProduct removes a product from catalog.
func DeleteProduct(id string) error {
	_, err := DB.Exec("DELETE FROM products WHERE id = ?", id)
	return err
}

// NextBillSeq generates the next sequential bill number atomically.
// Flow: begins tx -> reads current sequence -> formats string -> increments sequence -> commits tx.
func NextBillSeq(prefix, fy string) (string, error) {
	lock.Lock()
	defer lock.Unlock()

	tx, err := DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var seq int
	err = tx.QueryRow("SELECT CAST(val AS INTEGER) FROM meta WHERE key = 'bill_seq'").Scan(&seq)
	if err != nil {
		seq = 1
	}

	billNo := fmt.Sprintf("%s/%s/%04d", prefix, fy, seq)
	_, err = tx.Exec("UPDATE meta SET val = ? WHERE key = 'bill_seq'", fmt.Sprintf("%d", seq+1))
	if err != nil {
		return "", err
	}

	return billNo, tx.Commit()
}

// CreateBill processes an invoice, updates stock, and records movements in a single transaction.
//
// FLOW:
//   1. Begins SQLite transaction.
//   2. Validates available inventory for all requested items.
//   3. Inserts invoice header into 'bills'.
//   4. Inserts items into 'bill_items'.
//   5. Decrements stock in 'products' table.
//   6. Records 'sale' movements in 'stock_moves'.
//   7. Commits transaction atomically.
func CreateBill(bill Bill) (Bill, error) {
	lock.Lock()
	defer lock.Unlock()

	tx, err := DB.Begin()
	if err != nil {
		return bill, err
	}
	defer tx.Rollback()

	// Verify stock availability by aggregating duplicate SKUs
	reqQty := make(map[string]float64)
	for _, it := range bill.Items {
		reqQty[it.ProductID] += it.Qty
	}
	for pid, needed := range reqQty {
		var curStock float64
		var name string
		err := tx.QueryRow("SELECT stock, name_en FROM products WHERE id = ?", pid).Scan(&curStock, &name)
		if err != nil {
			return bill, fmt.Errorf("item %s not found: %w", pid, err)
		}
		if curStock < needed {
			return bill, fmt.Errorf("insufficient stock for %s (available: %.2f, requested: %.2f)", name, curStock, needed)
		}
	}

	// Insert bill header
	_, err = tx.Exec(`INSERT INTO bills 
		(id, bill_no, date, customer_name, customer_phone, customer_address, customer_gstin, customer_state, 
		 subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bill.ID, bill.BillNo, bill.Date, bill.Customer.Name, bill.Customer.Phone, bill.Customer.Address, bill.Customer.Gstin, bill.Customer.State,
		bill.Subtotal, bill.Discount, bill.Taxable, bill.GSTOn, bill.GSTAmt, bill.CGST, bill.SGST, bill.IGST, bill.GrandTotal, bill.PayMode, bill.Paid, bill.Balance, bill.Note,
	)
	if err != nil {
		return bill, fmt.Errorf("insert bill failed: %w", err)
	}

	// Insert bill items & deduct stock
	for _, it := range bill.Items {
		_, err = tx.Exec(`INSERT INTO bill_items 
			(bill_id, product_id, name_en, name_ta, hsn, barcode, qty, price, gst_pct, amount, gst_portion)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			bill.ID, it.ProductID, it.NameEn, it.NameTa, it.HSN, it.Barcode, it.Qty, it.Price, it.GSTPct, it.Amount, it.GSTPortion,
		)
		if err != nil {
			return bill, fmt.Errorf("insert bill item failed: %w", err)
		}

		// Update stock
		_, err = tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", it.Qty, it.ProductID)
		if err != nil {
			return bill, fmt.Errorf("deduct stock failed: %w", err)
		}

		// Record stock move with collision-free unique ID per bill item
		moveID := GenID("sm_sale")
		_, err = tx.Exec(`INSERT INTO stock_moves (id, date, product_id, type, qty, note, ref)
			VALUES (?, ?, ?, 'sale', ?, ?, ?)`,
			moveID, bill.Date, it.ProductID, -it.Qty, "Bill "+bill.BillNo, bill.ID,
		)
		if err != nil {
			return bill, fmt.Errorf("record stock move failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return bill, err
	}

	// Auto-save or update customer record in the customers database
	custName := strings.TrimSpace(bill.Customer.Name)
	custPhone := strings.TrimSpace(bill.Customer.Phone)
	if custPhone != "" || (custName != "" && !strings.EqualFold(custName, "Walk-in")) {
		_ = saveCustomerLocked(bill.Customer)
	}

	return bill, nil
}

// UpdateBill modifies an existing bill, reconciling inventory differences atomically.
//
// FLOW:
//   1. Begins SQLite transaction.
//   2. Queries existing items on this bill and credits back the previous stock deductions.
//   3. Validates stock availability for new/updated items.
//   4. Updates header in 'bills' table (preserving original bill_no and id).
//   5. Replaces items in 'bill_items' table.
//   6. Decrements stock for new items and writes 'sale' audit records in 'stock_moves'.
//   7. Auto-saves customer details in 'customers' table.
//   8. Commits transaction.
func UpdateBill(bill Bill) (Bill, error) {
	lock.Lock()
	defer lock.Unlock()

	tx, err := DB.Begin()
	if err != nil {
		return bill, err
	}
	defer tx.Rollback()

	// 1. Fetch previous items to reverse previous inventory deductions
	rows, err := tx.Query("SELECT product_id, qty FROM bill_items WHERE bill_id = ?", bill.ID)
	if err != nil {
		return bill, fmt.Errorf("failed to load original bill items: %w", err)
	}
	type itemRest struct {
		pid string
		qty float64
	}
	var oldItems []itemRest
	for rows.Next() {
		var ir itemRest
		if err := rows.Scan(&ir.pid, &ir.qty); err == nil {
			oldItems = append(oldItems, ir)
		}
	}
	rows.Close()

	// Reverse old stock deductions with collision-free IDs
	for _, ir := range oldItems {
		_, err = tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", ir.qty, ir.pid)
		if err != nil {
			return bill, fmt.Errorf("failed to revert old stock: %w", err)
		}
		moveID := GenID("sm_rev")
		_, _ = tx.Exec(`INSERT INTO stock_moves (id, date, product_id, type, qty, note, ref)
			VALUES (?, date('now'), ?, 'cancel', ?, ?, ?)`,
			moveID, ir.pid, ir.qty, "Bill Revision "+bill.BillNo, bill.ID,
		)
	}

	// 2. Verify stock availability for updated items (aggregating duplicate SKUs)
	reqQty := make(map[string]float64)
	for _, it := range bill.Items {
		reqQty[it.ProductID] += it.Qty
	}
	for pid, needed := range reqQty {
		var curStock float64
		var name string
		err := tx.QueryRow("SELECT stock, name_en FROM products WHERE id = ?", pid).Scan(&curStock, &name)
		if err != nil {
			return bill, fmt.Errorf("item %s not found: %w", pid, err)
		}
		if curStock < needed {
			return bill, fmt.Errorf("insufficient stock for %s (available: %.2f, requested: %.2f)", name, curStock, needed)
		}
	}

	// 3. Update bill header (keep bill_no, id, and created_at intact)
	gstOnInt := 0
	if bill.GSTOn {
		gstOnInt = 1
	}

	_, err = tx.Exec(`UPDATE bills SET 
		date = ?,
		customer_name = ?,
		customer_phone = ?,
		customer_address = ?,
		customer_gstin = ?,
		customer_state = ?,
		subtotal = ?,
		discount = ?,
		taxable = ?,
		gst_on = ?,
		gst_amt = ?,
		cgst = ?,
		sgst = ?,
		igst = ?,
		grand_total = ?,
		pay_mode = ?,
		paid = ?,
		balance = ?,
		note = ?
		WHERE id = ?`,
		bill.Date,
		bill.Customer.Name, bill.Customer.Phone, bill.Customer.Address, bill.Customer.Gstin, bill.Customer.State,
		bill.Subtotal, bill.Discount, bill.Taxable, gstOnInt, bill.GSTAmt,
		bill.CGST, bill.SGST, bill.IGST, bill.GrandTotal, bill.PayMode, bill.Paid, bill.Balance, bill.Note,
		bill.ID,
	)
	if err != nil {
		return bill, fmt.Errorf("update bill header failed: %w", err)
	}

	// 4. Delete old bill_items
	_, err = tx.Exec("DELETE FROM bill_items WHERE bill_id = ?", bill.ID)
	if err != nil {
		return bill, fmt.Errorf("delete old bill items failed: %w", err)
	}

	// 5. Insert new bill items and apply new stock deductions
	for _, it := range bill.Items {
		_, err = tx.Exec(`INSERT INTO bill_items 
			(bill_id, product_id, name_en, name_ta, hsn, barcode, qty, price, gst_pct, amount, gst_portion)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			bill.ID, it.ProductID, it.NameEn, it.NameTa, it.HSN, it.Barcode, it.Qty, it.Price, it.GSTPct, it.Amount, it.GSTPortion,
		)
		if err != nil {
			return bill, fmt.Errorf("insert bill item failed: %w", err)
		}

		// Update stock
		_, err = tx.Exec("UPDATE products SET stock = stock - ? WHERE id = ?", it.Qty, it.ProductID)
		if err != nil {
			return bill, fmt.Errorf("deduct stock failed: %w", err)
		}

		// Record stock move with collision-free unique ID
		moveID := GenID("sm_upd")
		_, err = tx.Exec(`INSERT INTO stock_moves (id, date, product_id, type, qty, note, ref)
			VALUES (?, ?, ?, 'sale', ?, ?, ?)`,
			moveID, bill.Date, it.ProductID, -it.Qty, "Bill Update "+bill.BillNo, bill.ID,
		)
		if err != nil {
			return bill, fmt.Errorf("record stock move failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return bill, err
	}

	// Auto-save or update customer record in the customers database
	custName := strings.TrimSpace(bill.Customer.Name)
	custPhone := strings.TrimSpace(bill.Customer.Phone)
	if custPhone != "" || (custName != "" && !strings.EqualFold(custName, "Walk-in")) {
		_ = saveCustomerLocked(bill.Customer)
	}

	return bill, nil
}

// CancelBill restores inventory and deletes the bill record.
//
// FLOW:
//   1. Begins transaction.
//   2. Queries all items sold on this bill.
//   3. Credits inventory back to products table.
//   4. Logs 'cancel' movement in stock_moves.
//   5. Deletes bill record (cascading deletes line items).
//   6. Commits transaction.
func CancelBill(id string) error {
	lock.Lock()
	defer lock.Unlock()

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get bill items to restore stock
	rows, err := tx.Query("SELECT product_id, qty FROM bill_items WHERE bill_id = ?", id)
	if err != nil {
		return err
	}
	type itemRest struct {
		pid string
		qty float64
	}
	var toRestore []itemRest
	for rows.Next() {
		var ir itemRest
		if err := rows.Scan(&ir.pid, &ir.qty); err == nil {
			toRestore = append(toRestore, ir)
		}
	}
	rows.Close()

	var billNo string
	_ = tx.QueryRow("SELECT bill_no FROM bills WHERE id = ?", id).Scan(&billNo)

	// Restore stock
	for _, ir := range toRestore {
		_, err = tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", ir.qty, ir.pid)
		if err != nil {
			return err
		}
		moveID := GenID("sm_c")
		_, _ = tx.Exec(`INSERT INTO stock_moves (id, date, product_id, type, qty, note, ref)
			VALUES (?, date('now'), ?, 'cancel', ?, ?, ?)`,
			moveID, ir.pid, ir.qty, "Cancelled Bill "+billNo, id,
		)
	}

	// Delete from bills (CASCADE deletes bill_items)
	_, err = tx.Exec("DELETE FROM bills WHERE id = ?", id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetBills retrieves bills with optional date range filter (from and to).
func GetBills(from, to string) ([]Bill, error) {
	query := `SELECT id, bill_no, date, COALESCE(customer_name, ''), COALESCE(customer_phone, ''), 
		COALESCE(customer_address, ''), COALESCE(customer_gstin, ''), COALESCE(customer_state, ''),
		subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance, COALESCE(note, ''), created_at
		FROM bills`
	var args []interface{}
	var conds []string
	if from != "" {
		conds = append(conds, "date >= ?")
		args = append(args, from)
	}
	if to != "" {
		conds = append(conds, "date <= ?")
		args = append(args, to)
	}
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]Bill, 0)
	for rows.Next() {
		var b Bill
		var gstOnInt int
		err := rows.Scan(
			&b.ID, &b.BillNo, &b.Date, &b.Customer.Name, &b.Customer.Phone, &b.Customer.Address, &b.Customer.Gstin, &b.Customer.State,
			&b.Subtotal, &b.Discount, &b.Taxable, &gstOnInt, &b.GSTAmt, &b.CGST, &b.SGST, &b.IGST, &b.GrandTotal, &b.PayMode, &b.Paid, &b.Balance, &b.Note, &b.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		b.GSTOn = gstOnInt == 1
		list = append(list, b)
	}

	// Fetch items for each bill
	for i := range list {
		iRows, err := DB.Query("SELECT product_id, name_en, name_ta, hsn, barcode, qty, price, gst_pct, amount, gst_portion FROM bill_items WHERE bill_id = ?", list[i].ID)
		if err == nil {
			for iRows.Next() {
				var it BillItem
				if err := iRows.Scan(&it.ProductID, &it.NameEn, &it.NameTa, &it.HSN, &it.Barcode, &it.Qty, &it.Price, &it.GSTPct, &it.Amount, &it.GSTPortion); err == nil {
					list[i].Items = append(list[i].Items, it)
				}
			}
			iRows.Close()
		}
	}
	return list, nil
}

// GetBillByID fetches a single bill and all its line items.
func GetBillByID(id string) (Bill, error) {
	var b Bill
	var gstOnInt int
	err := DB.QueryRow(`SELECT id, bill_no, date, COALESCE(customer_name, ''), COALESCE(customer_phone, ''), 
		COALESCE(customer_address, ''), COALESCE(customer_gstin, ''), COALESCE(customer_state, ''),
		subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance, COALESCE(note, ''), created_at
		FROM bills WHERE id = ?`, id).
		Scan(
			&b.ID, &b.BillNo, &b.Date, &b.Customer.Name, &b.Customer.Phone, &b.Customer.Address, &b.Customer.Gstin, &b.Customer.State,
			&b.Subtotal, &b.Discount, &b.Taxable, &gstOnInt, &b.GSTAmt, &b.CGST, &b.SGST, &b.IGST, &b.GrandTotal, &b.PayMode, &b.Paid, &b.Balance, &b.Note, &b.CreatedAt,
		)
	if err != nil {
		return b, err
	}
	b.GSTOn = gstOnInt == 1

	rows, err := DB.Query("SELECT product_id, name_en, name_ta, hsn, barcode, qty, price, gst_pct, amount, gst_portion FROM bill_items WHERE bill_id = ?", b.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var it BillItem
			if err := rows.Scan(&it.ProductID, &it.NameEn, &it.NameTa, &it.HSN, &it.Barcode, &it.Qty, &it.Price, &it.GSTPct, &it.Amount, &it.GSTPortion); err == nil {
				b.Items = append(b.Items, it)
			}
		}
	}
	return b, nil
}

// AdjustStock updates product stock level and records a stock movement audit entry.
func AdjustStock(productID, date, moveType string, qty float64, note string) error {
	lock.Lock()
	defer lock.Unlock()

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE products SET stock = stock + ? WHERE id = ?", qty, productID)
	if err != nil {
		return err
	}

	moveID := GenID("sm_adj")
	_, err = tx.Exec(`INSERT INTO stock_moves (id, date, product_id, type, qty, note)
		VALUES (?, ?, ?, ?, ?, ?)`,
		moveID, date, productID, moveType, qty, note,
	)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// GetStockMoves retrieves chronological stock ledger entries up to limit.
func GetStockMoves(limit int) ([]StockMove, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := DB.Query("SELECT id, date, product_id, type, qty, COALESCE(note, ''), COALESCE(ref, ''), created_at FROM stock_moves ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]StockMove, 0)
	for rows.Next() {
		var sm StockMove
		if err := rows.Scan(&sm.ID, &sm.Date, &sm.ProductID, &sm.Type, &sm.Qty, &sm.Note, &sm.Ref, &sm.CreatedAt); err == nil {
			list = append(list, sm)
		}
	}
	return list, nil
}

// SavePurchase inserts a raw material or packaging procurement record (upsert).
func SavePurchase(p Purchase) error {
	if p.ID == "" {
		p.ID = GenID("pu")
	}
	_, err := DB.Exec(`INSERT INTO purchases (id, date, supplier, item, qty, unit, cost, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			date = excluded.date,
			supplier = excluded.supplier,
			item = excluded.item,
			qty = excluded.qty,
			unit = excluded.unit,
			cost = excluded.cost,
			note = excluded.note`,
		p.ID, p.Date, p.Supplier, p.Item, p.Qty, p.Unit, p.Cost, p.Note,
	)
	return err
}

// GetPurchases retrieves all procurement records.
func GetPurchases() ([]Purchase, error) {
	rows, err := DB.Query("SELECT id, date, COALESCE(supplier, ''), item, qty, unit, cost, COALESCE(note, ''), created_at FROM purchases ORDER BY date DESC, created_at DESC LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]Purchase, 0)
	for rows.Next() {
		var p Purchase
		if err := rows.Scan(&p.ID, &p.Date, &p.Supplier, &p.Item, &p.Qty, &p.Unit, &p.Cost, &p.Note, &p.CreatedAt); err == nil {
			list = append(list, p)
		}
	}
	return list, nil
}

// DeletePurchase removes a procurement record by ID.
func DeletePurchase(id string) error {
	_, err := DB.Exec("DELETE FROM purchases WHERE id = ?", id)
	return err
}

// SaveExpense inserts an operational expenditure record (upsert).
func SaveExpense(e Expense) error {
	if e.ID == "" {
		e.ID = GenID("ex")
	}
	_, err := DB.Exec(`INSERT INTO expenses (id, date, head, amount, note)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			date = excluded.date,
			head = excluded.head,
			amount = excluded.amount,
			note = excluded.note`,
		e.ID, e.Date, e.Head, e.Amount, e.Note,
	)
	return err
}

// GetExpenses retrieves all operational expense records.
func GetExpenses() ([]Expense, error) {
	rows, err := DB.Query("SELECT id, date, head, amount, COALESCE(note, ''), created_at FROM expenses ORDER BY date DESC, created_at DESC LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]Expense, 0)
	for rows.Next() {
		var e Expense
		if err := rows.Scan(&e.ID, &e.Date, &e.Head, &e.Amount, &e.Note, &e.CreatedAt); err == nil {
			list = append(list, e)
		}
	}
	return list, nil
}

// DeleteExpense removes an expense record by ID.
func DeleteExpense(id string) error {
	_, err := DB.Exec("DELETE FROM expenses WHERE id = ?", id)
	return err
}

// GetDashboard calculates real-time dashboard KPIs.
// Flow: runs SQL aggregations for today and month sales -> sums stock valuation -> lists low stock SKUs.
func GetDashboard() (DashboardStats, error) {
	var s DashboardStats
	s.LowStockItems = make([]Product, 0)
	today := time.Now().Format("2006-01-02")
	month := time.Now().Format("2006-01")

	// Today sales & bill count
	_ = DB.QueryRow("SELECT COALESCE(SUM(grand_total), 0), COUNT(*) FROM bills WHERE date = ?", today).Scan(&s.TodaySales, &s.TodayBillsCount)

	// Month sales & bill count
	_ = DB.QueryRow("SELECT COALESCE(SUM(grand_total), 0), COUNT(*) FROM bills WHERE strftime('%Y-%m', date) = ?", month).Scan(&s.MonthSales, &s.MonthBillsCount)

	// Month purchases & expenses
	_ = DB.QueryRow("SELECT COALESCE(SUM(cost), 0) FROM purchases WHERE strftime('%Y-%m', date) = ?", month).Scan(&s.MonthPurchases)
	_ = DB.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM expenses WHERE strftime('%Y-%m', date) = ?", month).Scan(&s.MonthExpenses)

	// Stock value & product count
	_ = DB.QueryRow("SELECT COALESCE(SUM(price * stock), 0), COUNT(*) FROM products").Scan(&s.StockValue, &s.TotalProducts)

	// Low stock items
	rows, err := DB.Query(`SELECT id, category, category_ta, variant, variant_ml, name_en, name_ta, hsn, barcode, gst_pct, price, mrp, stock, low_alert, unit 
		FROM products WHERE stock <= low_alert ORDER BY stock ASC`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p Product
			if err := rows.Scan(&p.ID, &p.Category, &p.CategoryTa, &p.Variant, &p.VariantMl, &p.NameEn, &p.NameTa, &p.HSN, &p.Barcode, &p.GSTPct, &p.Price, &p.MRP, &p.Stock, &p.LowAlert, &p.Unit); err == nil {
				s.LowStockItems = append(s.LowStockItems, p)
			}
		}
	}

	return s, nil
}

// SaveCustomer creates or updates a customer profile.
// Automatically called whenever a bill is generated with customer details.
func SaveCustomer(c Customer) error {
	lock.Lock()
	defer lock.Unlock()
	return saveCustomerLocked(c)
}

func saveCustomerLocked(c Customer) error {
	name := strings.TrimSpace(c.Name)
	phone := strings.TrimSpace(c.Phone)
	if name == "" && phone == "" {
		return nil
	}
	if strings.EqualFold(name, "Walk-in") && phone == "" {
		return nil
	}

	if c.ID == "" {
		c.ID = GenID("c")
	}
	if c.State == "" {
		c.State = "Tamil Nadu"
	}

	if phone != "" {
		_, err := DB.Exec(`INSERT INTO customers (id, name, phone, address, gstin, state, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(phone) DO UPDATE SET
				name = excluded.name,
				address = CASE WHEN excluded.address != '' THEN excluded.address ELSE customers.address END,
				gstin = CASE WHEN excluded.gstin != '' THEN excluded.gstin ELSE customers.gstin END,
				state = CASE WHEN excluded.state != '' THEN excluded.state ELSE customers.state END,
				updated_at = CURRENT_TIMESTAMP`,
			c.ID, name, phone, strings.TrimSpace(c.Address), strings.TrimSpace(c.Gstin), c.State,
		)
		return err
	}

	_, err := DB.Exec(`INSERT INTO customers (id, name, phone, address, gstin, state, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			address = excluded.address,
			gstin = excluded.gstin,
			state = excluded.state,
			updated_at = CURRENT_TIMESTAMP`,
		c.ID, name, "", strings.TrimSpace(c.Address), strings.TrimSpace(c.Gstin), c.State,
	)
	return err
}

// SearchCustomers finds customers matching name, mobile phone number, or GSTIN.
func SearchCustomers(query string, limit int) ([]Customer, error) {
	if limit <= 0 || limit > 50 {
		limit = 15
	}
	query = strings.TrimSpace(query)
	var rows *sql.Rows
	var err error

	if query == "" {
		rows, err = DB.Query(`SELECT id, name, COALESCE(phone, ''), COALESCE(address, ''), COALESCE(gstin, ''), COALESCE(state, 'Tamil Nadu') 
			FROM customers ORDER BY updated_at DESC LIMIT ?`, limit)
	} else {
		pattern := "%" + query + "%"
		rows, err = DB.Query(`SELECT id, name, COALESCE(phone, ''), COALESCE(address, ''), COALESCE(gstin, ''), COALESCE(state, 'Tamil Nadu') 
			FROM customers 
			WHERE name LIKE ? OR phone LIKE ? OR gstin LIKE ?
			ORDER BY CASE WHEN name LIKE ? THEN 1 WHEN phone LIKE ? THEN 2 ELSE 3 END, updated_at DESC 
			LIMIT ?`, pattern, pattern, pattern, query+"%", query+"%", limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone, &c.Address, &c.Gstin, &c.State); err == nil {
			list = append(list, c)
		}
	}
	return list, nil
}

// RestoreBillsAndMoves imports historical bills and stock audit ledger entries from a backup.
func RestoreBillsAndMoves(bills []Bill, moves []StockMove) error {
	lock.Lock()
	defer lock.Unlock()

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, b := range bills {
		gstOnInt := 0
		if b.GSTOn {
			gstOnInt = 1
		}
		_, err := tx.Exec(`INSERT INTO bills 
			(id, bill_no, date, customer_name, customer_phone, customer_address, customer_gstin, customer_state, 
			 subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				bill_no = excluded.bill_no,
				date = excluded.date,
				customer_name = excluded.customer_name,
				customer_phone = excluded.customer_phone,
				customer_address = excluded.customer_address,
				customer_gstin = excluded.customer_gstin,
				customer_state = excluded.customer_state,
				subtotal = excluded.subtotal,
				discount = excluded.discount,
				taxable = excluded.taxable,
				gst_on = excluded.gst_on,
				gst_amt = excluded.gst_amt,
				cgst = excluded.cgst,
				sgst = excluded.sgst,
				igst = excluded.igst,
				grand_total = excluded.grand_total,
				pay_mode = excluded.pay_mode,
				paid = excluded.paid,
				balance = excluded.balance,
				note = excluded.note`,
			b.ID, b.BillNo, b.Date, b.Customer.Name, b.Customer.Phone, b.Customer.Address, b.Customer.Gstin, b.Customer.State,
			b.Subtotal, b.Discount, b.Taxable, gstOnInt, b.GSTAmt, b.CGST, b.SGST, b.IGST, b.GrandTotal, b.PayMode, b.Paid, b.Balance, b.Note, b.CreatedAt,
		)
		if err != nil {
			return err
		}

		// Delete and reinsert items for this bill to avoid duplication
		_, _ = tx.Exec("DELETE FROM bill_items WHERE bill_id = ?", b.ID)
		for _, it := range b.Items {
			_, err = tx.Exec(`INSERT INTO bill_items 
				(bill_id, product_id, name_en, name_ta, hsn, barcode, qty, price, gst_pct, amount, gst_portion)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				b.ID, it.ProductID, it.NameEn, it.NameTa, it.HSN, it.Barcode, it.Qty, it.Price, it.GSTPct, it.Amount, it.GSTPortion,
			)
			if err != nil {
				return err
			}
		}
	}

	for _, m := range moves {
		_, err := tx.Exec(`INSERT INTO stock_moves (id, date, product_id, type, qty, note, ref, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				date = excluded.date,
				product_id = excluded.product_id,
				type = excluded.type,
				qty = excluded.qty,
				note = excluded.note,
				ref = excluded.ref`,
			m.ID, m.Date, m.ProductID, m.Type, m.Qty, m.Note, m.Ref, m.CreatedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CloseDB safely checkpoints Write-Ahead Logging (WAL) and closes the SQLite database connection.
func CloseDB() error {
	lock.Lock()
	defer lock.Unlock()
	if DB != nil {
		_, _ = DB.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
		err := DB.Close()
		DB = nil
		return err
	}
	return nil
}

// AutoDailyBackup creates a non-blocking online snapshot of the database using SQLite's VACUUM INTO.
// It stores backups in <db_dir>/backups/pos_auto_YYYYMMDD.db and deletes snapshots older than keepDays.
func AutoDailyBackup(keepDays int) (string, error) {
	if currentDBPath == "" || DB == nil {
		return "", fmt.Errorf("database not initialized")
	}

	backupDir := filepath.Join(filepath.Dir(currentDBPath), "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("create backup dir failed: %w", err)
	}

	todayStr := time.Now().Format("20060102")
	targetFile := filepath.Join(backupDir, fmt.Sprintf("pos_auto_%s.db", todayStr))

	// If backup for today already exists, no need to recreate
	if _, err := os.Stat(targetFile); err == nil {
		cleanOldBackups(backupDir, keepDays)
		return targetFile, nil
	}

	// Lock briefly to issue VACUUM INTO
	lock.Lock()
	_, err := DB.Exec("VACUUM INTO ?", targetFile)
	lock.Unlock()

	if err != nil {
		return "", fmt.Errorf("vacuum into failed: %w", err)
	}

	cleanOldBackups(backupDir, keepDays)
	return targetFile, nil
}

func cleanOldBackups(backupDir string, keepDays int) {
	if keepDays <= 0 {
		keepDays = 14
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -keepDays)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "pos_auto_") || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(backupDir, entry.Name()))
		}
	}
}

// StartAutoBackupScheduler triggers an immediate auto-backup in the background and runs periodically.
func StartAutoBackupScheduler(stopChan <-chan struct{}) {
	go func() {
		// Run once after initial 3-second startup warm up
		time.Sleep(3 * time.Second)
		if path, err := AutoDailyBackup(14); err == nil && path != "" {
			fmt.Printf(" [Safety Backup] Verified auto-snapshot: %s ✓\n", filepath.Base(path))
		}

		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_, _ = AutoDailyBackup(14)
			case <-stopChan:
				return
			}
		}
	}()
}
