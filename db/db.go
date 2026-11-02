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
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

func roundMoney(val float64) float64 {
	return math.Round(val*100) / 100
}

func getBillItemsLocked(billID string) []BillItem {
	rows, err := DB.Query("SELECT product_id, name_en, name_ta, hsn, barcode, qty, price, gst_pct, amount, gst_portion FROM bill_items WHERE bill_id = ?", billID)
	if err != nil {
		return []BillItem{}
	}
	defer rows.Close()
	var items []BillItem
	for rows.Next() {
		var it BillItem
		if err := rows.Scan(&it.ProductID, &it.NameEn, &it.NameTa, &it.HSN, &it.Barcode, &it.Qty, &it.Price, &it.GSTPct, &it.Amount, &it.GSTPortion); err == nil {
			items = append(items, it)
		}
	}
	return items
}

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
		cash_paid REAL DEFAULT 0,
		upi_paid REAL DEFAULT 0,
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

	CREATE TABLE IF NOT EXISTS categories (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		name_ta TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS customer_payments (
		id TEXT PRIMARY KEY,
		receipt_no TEXT NOT NULL,
		date TEXT NOT NULL,
		customer_name TEXT NOT NULL,
		customer_phone TEXT DEFAULT '',
		amount REAL NOT NULL,
		pay_mode TEXT NOT NULL,
		bank_name TEXT DEFAULT '',
		ref_no TEXT DEFAULT '',
		note TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_bills_date ON bills(date);
	CREATE INDEX IF NOT EXISTS idx_stock_moves_pid ON stock_moves(product_id);
	CREATE INDEX IF NOT EXISTS idx_purchases_date ON purchases(date);
	CREATE INDEX IF NOT EXISTS idx_expenses_date ON expenses(date);
	CREATE INDEX IF NOT EXISTS idx_customers_phone ON customers(phone);
	CREATE INDEX IF NOT EXISTS idx_customers_name ON customers(name);
	CREATE INDEX IF NOT EXISTS idx_bills_customer_phone ON bills(customer_phone);
	CREATE INDEX IF NOT EXISTS idx_bill_items_pid ON bill_items(product_id);
	CREATE INDEX IF NOT EXISTS idx_customer_payments_phone ON customer_payments(customer_phone);
	CREATE INDEX IF NOT EXISTS idx_customer_payments_name ON customer_payments(customer_name);
	CREATE INDEX IF NOT EXISTS idx_customer_payments_date ON customer_payments(date);
	`
	_, err := DB.Exec(schema)
	if err != nil {
		return err
	}
	_, _ = DB.Exec("ALTER TABLE bills ADD COLUMN cash_paid REAL DEFAULT 0")
	_, _ = DB.Exec("ALTER TABLE bills ADD COLUMN upi_paid REAL DEFAULT 0")
	return nil
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

	// Seed default product categories if empty
	var catCount int
	if err := DB.QueryRow("SELECT COUNT(*) FROM categories").Scan(&catCount); err == nil && catCount == 0 {
		defaultCats := []struct {
			id, name, nameTa string
		}{
			{"cat_groundnut", "Groundnut", "நிலக்கடலை"},
			{"cat_sesame", "Sesame", "எள்"},
			{"cat_coconut", "Coconut", "தேங்காய்"},
			{"cat_cakes", "Oil Cakes", "புண்ணாக்கு"},
			{"cat_lamp", "Lamp Oil", "விளக்கு எண்ணெய்"},
			{"cat_seeds", "Seeds", "விதை"},
			{"cat_millets", "Millets", "தானியங்கள்"},
			{"cat_other", "Other", "மற்றவை"},
		}
		for _, c := range defaultCats {
			_, _ = DB.Exec("INSERT OR IGNORE INTO categories (id, name, name_ta) VALUES (?, ?, ?)", c.id, c.name, c.nameTa)
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
	if DB == nil {
		s.MasterPin = "1234"
		return s, nil
	}
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
	if DB == nil {
		return nil
	}
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
	if err == nil && strings.TrimSpace(p.Category) != "" {
		_ = SaveCategory(Category{Name: strings.TrimSpace(p.Category), NameTa: strings.TrimSpace(p.CategoryTa)})
	}
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

	if bill.ID == "" {
		bill.ID = GenID("b")
	}
	if bill.BillNo == "" {
		bill.BillNo = GenID("BILL")
	}

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

	if bill.PayMode == "Split" || (bill.CashPaid > 0 && bill.UPIPaid > 0) {
		bill.PayMode = "Split"
		if bill.Paid == 0 {
			bill.Paid = bill.CashPaid + bill.UPIPaid
		}
	} else if bill.CashPaid == 0 && bill.UPIPaid == 0 {
		if bill.PayMode == "Cash" {
			bill.CashPaid = bill.Paid
		} else if bill.PayMode == "UPI" {
			bill.UPIPaid = bill.Paid
		}
	}

	// Insert bill header
	_, err = tx.Exec(`INSERT INTO bills 
		(id, bill_no, date, customer_name, customer_phone, customer_address, customer_gstin, customer_state, 
		 subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance, cash_paid, upi_paid, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bill.ID, bill.BillNo, bill.Date, bill.Customer.Name, bill.Customer.Phone, bill.Customer.Address, bill.Customer.Gstin, bill.Customer.State,
		bill.Subtotal, bill.Discount, bill.Taxable, bill.GSTOn, bill.GSTAmt, bill.CGST, bill.SGST, bill.IGST, bill.GrandTotal, bill.PayMode, bill.Paid, bill.Balance, bill.CashPaid, bill.UPIPaid, bill.Note,
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

	if bill.PayMode == "Split" || (bill.CashPaid > 0 && bill.UPIPaid > 0) {
		bill.PayMode = "Split"
		if bill.Paid == 0 {
			bill.Paid = bill.CashPaid + bill.UPIPaid
		}
	} else if bill.CashPaid == 0 && bill.UPIPaid == 0 {
		if bill.PayMode == "Cash" {
			bill.CashPaid = bill.Paid
		} else if bill.PayMode == "UPI" {
			bill.UPIPaid = bill.Paid
		}
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
		cash_paid = ?,
		upi_paid = ?,
		note = ?
		WHERE id = ?`,
		bill.Date,
		bill.Customer.Name, bill.Customer.Phone, bill.Customer.Address, bill.Customer.Gstin, bill.Customer.State,
		bill.Subtotal, bill.Discount, bill.Taxable, gstOnInt, bill.GSTAmt,
		bill.CGST, bill.SGST, bill.IGST, bill.GrandTotal, bill.PayMode, bill.Paid, bill.Balance, bill.CashPaid, bill.UPIPaid, bill.Note,
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
		subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance,
		COALESCE(cash_paid, 0), COALESCE(upi_paid, 0), COALESCE(note, ''), created_at
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
			&b.Subtotal, &b.Discount, &b.Taxable, &gstOnInt, &b.GSTAmt, &b.CGST, &b.SGST, &b.IGST, &b.GrandTotal, &b.PayMode, &b.Paid, &b.Balance, &b.CashPaid, &b.UPIPaid, &b.Note, &b.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		b.GSTOn = gstOnInt == 1
		if b.CashPaid == 0 && b.UPIPaid == 0 {
			if b.PayMode == "Cash" {
				b.CashPaid = b.Paid
			} else if b.PayMode == "UPI" {
				b.UPIPaid = b.Paid
			}
		}
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
		subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance,
		COALESCE(cash_paid, 0), COALESCE(upi_paid, 0), COALESCE(note, ''), created_at
		FROM bills WHERE id = ?`, id).
		Scan(
			&b.ID, &b.BillNo, &b.Date, &b.Customer.Name, &b.Customer.Phone, &b.Customer.Address, &b.Customer.Gstin, &b.Customer.State,
			&b.Subtotal, &b.Discount, &b.Taxable, &gstOnInt, &b.GSTAmt, &b.CGST, &b.SGST, &b.IGST, &b.GrandTotal, &b.PayMode, &b.Paid, &b.Balance, &b.CashPaid, &b.UPIPaid, &b.Note, &b.CreatedAt,
		)
	if err != nil {
		return b, err
	}
	b.GSTOn = gstOnInt == 1
	if b.CashPaid == 0 && b.UPIPaid == 0 {
		if b.PayMode == "Cash" {
			b.CashPaid = b.Paid
		} else if b.PayMode == "UPI" {
			b.UPIPaid = b.Paid
		}
	}

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

	// If customer already exists by ID, update directly
	var existingID string
	err := DB.QueryRow("SELECT id FROM customers WHERE id = ?", c.ID).Scan(&existingID)
	if err == nil && existingID != "" {
		_, err = DB.Exec(`UPDATE customers SET 
			name = ?,
			phone = ?,
			address = ?,
			gstin = ?,
			state = ?,
			updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			name, phone, strings.TrimSpace(c.Address), strings.TrimSpace(c.Gstin), c.State, c.ID,
		)
		return err
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

	_, err = DB.Exec(`INSERT INTO customers (id, name, phone, address, gstin, state, updated_at)
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

// DeleteCustomer removes a customer profile by ID or phone number.
func DeleteCustomer(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("customer ID is required")
	}
	_, err := DB.Exec("DELETE FROM customers WHERE id = ? OR phone = ?", id, id)
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

// GetCategories returns all available product categories (both predefined and custom created) with product counts.
func GetCategories() ([]Category, error) {
	rows, err := DB.Query(`
		SELECT c.id, c.name, COALESCE(c.name_ta, ''), COUNT(p.id) as pcount
		FROM categories c
		LEFT JOIN products p ON LOWER(TRIM(c.name)) = LOWER(TRIM(p.category))
		GROUP BY c.id, c.name, c.name_ta
		ORDER BY CASE 
			WHEN c.name = 'Groundnut' THEN 1
			WHEN c.name = 'Sesame' THEN 2
			WHEN c.name = 'Coconut' THEN 3
			WHEN c.name = 'Oil Cakes' THEN 4
			WHEN c.name = 'Lamp Oil' THEN 5
			WHEN c.name = 'Seeds' THEN 6
			WHEN c.name = 'Millets' THEN 7
			WHEN c.name = 'Other' THEN 99
			ELSE 10 
		END, c.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.NameTa, &c.ProductCount); err == nil {
			list = append(list, c)
		}
	}

	// Also include any distinct category in products table not yet in categories
	pRows, err := DB.Query("SELECT DISTINCT category FROM products WHERE category != ''")
	if err == nil {
		defer pRows.Close()
		existing := make(map[string]bool)
		for _, c := range list {
			existing[strings.ToLower(c.Name)] = true
		}
		for pRows.Next() {
			var pCat string
			if err := pRows.Scan(&pCat); err == nil && !existing[strings.ToLower(pCat)] {
				var count int
				_ = DB.QueryRow("SELECT COUNT(*) FROM products WHERE LOWER(TRIM(category)) = LOWER(TRIM(?))", pCat).Scan(&count)
				list = append(list, Category{
					ID:           GenID("cat"),
					Name:         pCat,
					ProductCount: count,
				})
				existing[strings.ToLower(pCat)] = true
			}
		}
	}

	return list, nil
}

// SaveCategory creates or updates a custom category.
// If an existing category is renamed, all products using that category are automatically updated.
func SaveCategory(c Category) error {
	name := strings.TrimSpace(c.Name)
	nameTa := strings.TrimSpace(c.NameTa)
	if name == "" {
		return errors.New("category name is required")
	}

	// If ID is provided, check if existing
	if c.ID != "" {
		var oldName, oldNameTa string
		err := DB.QueryRow("SELECT name, COALESCE(name_ta, '') FROM categories WHERE id = ?", c.ID).Scan(&oldName, &oldNameTa)
		if err == nil && oldName != "" {
			// Update category record
			_, err = DB.Exec("UPDATE categories SET name = ?, name_ta = ? WHERE id = ?", name, nameTa, c.ID)
			if err != nil {
				return err
			}
			// Update products using the old category name
			if !strings.EqualFold(oldName, name) || (nameTa != "" && oldNameTa != nameTa) {
				_, _ = DB.Exec("UPDATE products SET category = ?, category_ta = ? WHERE LOWER(TRIM(category)) = LOWER(TRIM(?))", name, nameTa, oldName)
			}
			return nil
		}
	}

	if c.ID == "" {
		c.ID = GenID("cat")
	}

	_, err := DB.Exec(`INSERT INTO categories (id, name, name_ta) VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET name_ta = CASE WHEN excluded.name_ta != '' THEN excluded.name_ta ELSE categories.name_ta END`,
		c.ID, name, nameTa,
	)
	return err
}

// DeleteCategory deletes a category by ID or Name and safely reassigns products to 'Other'.
func DeleteCategory(idOrName string) error {
	idOrName = strings.TrimSpace(idOrName)
	if idOrName == "" {
		return errors.New("category ID or name is required")
	}

	var catName string
	_ = DB.QueryRow("SELECT name FROM categories WHERE id = ? OR name = ?", idOrName, idOrName).Scan(&catName)
	if catName == "" {
		catName = idOrName
	}

	// Reassign products to Other so inventory is never lost
	_, _ = DB.Exec("UPDATE products SET category = 'Other', category_ta = 'மற்றவை' WHERE LOWER(TRIM(category)) = LOWER(TRIM(?))", catName)

	_, err := DB.Exec("DELETE FROM categories WHERE id = ? OR name = ?", idOrName, idOrName)
	return err
}

// LookupCustomer checks a phone number to distinguish First-Time vs Registered returning customers during billing.
func LookupCustomer(phone string) (*CustomerLookupResult, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return &CustomerLookupResult{Found: false, IsRegistered: false}, nil
	}

	res := &CustomerLookupResult{
		Found:        false,
		IsRegistered: false,
	}

	// 1. Check customers table
	var c Customer
	err := DB.QueryRow(`SELECT id, name, COALESCE(phone, ''), COALESCE(address, ''),
		COALESCE(gstin, ''), COALESCE(state, 'Tamil Nadu') 
		FROM customers WHERE phone = ?`, phone).
		Scan(&c.ID, &c.Name, &c.Phone, &c.Address, &c.Gstin, &c.State)

	if err == nil {
		res.Found = true
		res.Customer = c
	} else {
		// Also check recent bill for this phone if customer table was missing it
		var bCustName, bCustGstin, bCustAddr, bCustState sql.NullString
		bErr := DB.QueryRow(`SELECT customer_name, customer_gstin, customer_address, customer_state 
			FROM bills WHERE customer_phone = ? ORDER BY date DESC, id DESC LIMIT 1`, phone).
			Scan(&bCustName, &bCustGstin, &bCustAddr, &bCustState)
		if bErr == nil && bCustName.Valid && bCustName.String != "" {
			res.Found = true
			res.Customer = Customer{
				ID:      GenID("cust"),
				Name:    bCustName.String,
				Phone:   phone,
				Address: bCustAddr.String,
				Gstin:   bCustGstin.String,
				State:   bCustState.String,
			}
		}
	}

	// 2. Count prior bills and total spend
	var billsCount int
	var totalSpend sql.NullFloat64
	var lastDate sql.NullString
	_ = DB.QueryRow(`SELECT COUNT(*), SUM(grand_total), MAX(date) FROM bills WHERE customer_phone = ?`, phone).
		Scan(&billsCount, &totalSpend, &lastDate)

	res.TotalBillsCount = billsCount
	if totalSpend.Valid {
		res.TotalSpent = roundMoney(totalSpend.Float64)
	}
	if lastDate.Valid {
		res.LastVisitDate = lastDate.String
	}

	// Registration Rule:
	// A customer who has at least one previous bill is "Registered".
	// A first-time user who has never billed before is "Unregistered / New Customer".
	res.IsRegistered = billsCount > 0

	// 3. Find top 3 frequently purchased items
	topRows, topErr := DB.Query(`
		SELECT bi.name_en 
		FROM bill_items bi 
		JOIN bills b ON bi.bill_id = b.id 
		WHERE b.customer_phone = ? 
		GROUP BY bi.name_en 
		ORDER BY SUM(bi.qty) DESC 
		LIMIT 3`, phone)
	if topErr == nil {
		defer topRows.Close()
		for topRows.Next() {
			var pName string
			if err := topRows.Scan(&pName); err == nil && pName != "" {
				res.TopProducts = append(res.TopProducts, pName)
			}
		}
	}

	return res, nil
}

// GetCustomerHistory fetches full customer purchase CRM details and item-wise purchase history.
func GetCustomerHistory(query string, from string, to string) (*CustomerHistory, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("customer phone or name is required")
	}

	// 1. Resolve Customer Profile
	var c Customer
	err := DB.QueryRow(`
		SELECT id, name, COALESCE(phone, ''), COALESCE(address, ''), COALESCE(gstin, ''), COALESCE(state, 'Tamil Nadu')
		FROM customers 
		WHERE phone = ? OR name = ? OR id = ?
		LIMIT 1`, query, query, query).
		Scan(&c.ID, &c.Name, &c.Phone, &c.Address, &c.Gstin, &c.State)

	if err != nil {
		// Fallback to bills
		var bName, bPhone, bGstin, bAddr, bState sql.NullString
		bErr := DB.QueryRow(`
			SELECT customer_name, customer_phone, customer_gstin, customer_address, customer_state
			FROM bills 
			WHERE customer_phone = ? OR customer_name = ?
			ORDER BY date DESC LIMIT 1`, query, query).
			Scan(&bName, &bPhone, &bGstin, &bAddr, &bState)
		if bErr != nil {
			return nil, errors.New("customer not found")
		}
		c = Customer{
			ID:      GenID("cust"),
			Name:    bName.String,
			Phone:   bPhone.String,
			Address: bAddr.String,
			Gstin:   bGstin.String,
			State:   bState.String,
		}
	}

	hist := &CustomerHistory{
		Customer:       c,
		Bills:          []Bill{},
		ProductSummary: []CustomerProductSummary{},
	}

	phone := c.Phone

	// Lifetime stats
	var lifetimeBills int
	var lifetimeSpent sql.NullFloat64
	var minDate, maxDate sql.NullString
	_ = DB.QueryRow(`SELECT COUNT(*), SUM(grand_total), MIN(date), MAX(date) FROM bills WHERE (customer_phone = ? AND customer_phone != '') OR customer_name = ?`, phone, c.Name).
		Scan(&lifetimeBills, &lifetimeSpent, &minDate, &maxDate)

	hist.IsRegistered = lifetimeBills > 0
	if minDate.Valid {
		hist.FirstVisitDate = minDate.String
	}
	if maxDate.Valid {
		hist.LastVisitDate = maxDate.String
	}

	// 2. Filtered Bills Query
	bQuery := `SELECT id, bill_no, date, customer_name, customer_phone, customer_address, customer_gstin, customer_state,
			subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance, note,
			COALESCE(cash_paid, 0), COALESCE(upi_paid, 0)
		FROM bills 
		WHERE ((customer_phone = ? AND customer_phone != '') OR customer_name = ?)`
	var args []interface{}
	args = append(args, phone, c.Name)

	if from != "" {
		bQuery += " AND date >= ?"
		args = append(args, from)
	}
	if to != "" {
		bQuery += " AND date <= ?"
		args = append(args, to)
	}
	bQuery += " ORDER BY date DESC, bill_no DESC"

	bRows, err := DB.Query(bQuery, args...)
	if err != nil {
		return nil, err
	}
	defer bRows.Close()

	var totalPeriodSpent float64
	for bRows.Next() {
		var b Bill
		var gstOnInt int
		err := bRows.Scan(
			&b.ID, &b.BillNo, &b.Date, &b.Customer.Name, &b.Customer.Phone, &b.Customer.Address, &b.Customer.Gstin, &b.Customer.State,
			&b.Subtotal, &b.Discount, &b.Taxable, &gstOnInt, &b.GSTAmt, &b.CGST, &b.SGST, &b.IGST, &b.GrandTotal, &b.PayMode, &b.Paid, &b.Balance, &b.Note,
			&b.CashPaid, &b.UPIPaid,
		)
		if err == nil {
			b.GSTOn = gstOnInt == 1
			b.Items = getBillItemsLocked(b.ID)
			hist.Bills = append(hist.Bills, b)
			totalPeriodSpent += b.GrandTotal
		}
	}

	hist.TotalBillsCount = len(hist.Bills)
	hist.TotalSpent = roundMoney(totalPeriodSpent)
	if hist.TotalBillsCount > 0 {
		hist.AvgBillValue = roundMoney(hist.TotalSpent / float64(hist.TotalBillsCount))
	}

	// 3. Product-Wise Purchase Summary (What products they purchased & spend)
	pQuery := `
		SELECT bi.product_id, bi.name_en, bi.name_ta, COALESCE(p.category, 'Other'),
			SUM(bi.qty) as total_qty, SUM(bi.amount) as total_spent,
			COUNT(DISTINCT b.id) as orders_count, MAX(b.date) as last_date
		FROM bill_items bi
		JOIN bills b ON bi.bill_id = b.id
		LEFT JOIN products p ON bi.product_id = p.id
		WHERE ((b.customer_phone = ? AND b.customer_phone != '') OR b.customer_name = ?)`
	var pArgs []interface{}
	pArgs = append(pArgs, phone, c.Name)

	if from != "" {
		pQuery += " AND b.date >= ?"
		pArgs = append(pArgs, from)
	}
	if to != "" {
		pQuery += " AND b.date <= ?"
		pArgs = append(pArgs, to)
	}
	pQuery += " GROUP BY bi.name_en ORDER BY total_spent DESC"

	pRows, pErr := DB.Query(pQuery, pArgs...)
	if pErr == nil {
		defer pRows.Close()
		for pRows.Next() {
			var ps CustomerProductSummary
			if err := pRows.Scan(&ps.ProductID, &ps.NameEn, &ps.NameTa, &ps.Category, &ps.TotalQty, &ps.TotalSpent, &ps.OrdersCount, &ps.LastPurchasedDate); err == nil {
				ps.TotalSpent = roundMoney(ps.TotalSpent)
				hist.ProductSummary = append(hist.ProductSummary, ps)
			}
		}
	}

	return hist, nil
}

// GetCustomerDirectory retrieves directory of all customers with order counts, spend, and ledger balance.
func GetCustomerDirectory(query string, limit int) ([]CustomerDirectoryItem, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query = strings.TrimSpace(query)

	qStr := `
		SELECT 
			c.id, c.name, COALESCE(c.phone, ''), COALESCE(c.address, ''), COALESCE(c.gstin, ''), COALESCE(c.state, 'Tamil Nadu'),
			COUNT(DISTINCT b.id) as bills_count,
			COALESCE(SUM(b.grand_total), 0) as total_spent,
			COALESCE(MIN(b.date), '') as first_visit,
			COALESCE(MAX(b.date), '') as last_visit,
			ROUND(
				COALESCE((SELECT SUM(b2.grand_total) - SUM(b2.paid) FROM bills b2 WHERE (b2.customer_phone = c.phone AND c.phone != '') OR b2.customer_name = c.name), 0) -
				COALESCE((SELECT SUM(p.amount) FROM customer_payments p WHERE (p.customer_phone = c.phone AND c.phone != '') OR p.customer_name = c.name), 0)
			, 2) as balance
		FROM customers c
		LEFT JOIN bills b ON (c.phone = b.customer_phone AND c.phone != '') OR c.name = b.customer_name`

	var args []interface{}
	if query != "" {
		pattern := "%" + query + "%"
		qStr += " WHERE c.name LIKE ? OR c.phone LIKE ? OR c.gstin LIKE ?"
		args = append(args, pattern, pattern, pattern)
	}

	qStr += ` GROUP BY c.id ORDER BY total_spent DESC, bills_count DESC LIMIT ?`
	args = append(args, limit)

	rows, err := DB.Query(qStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CustomerDirectoryItem
	for rows.Next() {
		var item CustomerDirectoryItem
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Phone, &item.Address, &item.Gstin, &item.State,
			&item.BillsCount, &item.TotalSpent, &item.FirstVisit, &item.LastVisit, &item.Balance,
		); err == nil {
			item.TotalSpent = roundMoney(item.TotalSpent)
			item.Balance = roundMoney(item.Balance)
			item.IsRegistered = item.BillsCount > 0
			list = append(list, item)
		}
	}
	return list, nil
}

// SaveCustomerPayment records a formal cash, bank transfer, UPI or cheque receipt from a customer.
func SaveCustomerPayment(p CustomerPayment) (CustomerPayment, error) {
	name := strings.TrimSpace(p.CustomerName)
	phone := strings.TrimSpace(p.CustomerPhone)
	if name == "" && phone == "" {
		return p, errors.New("customer name or phone is required")
	}
	if p.Amount <= 0 {
		return p, errors.New("payment amount must be greater than zero")
	}
	if p.ID == "" {
		p.ID = GenID("cpmt")
	}
	if p.ReceiptNo == "" {
		p.ReceiptNo = fmt.Sprintf("RCPT-%s-%04d", time.Now().Format("20060102"), time.Now().Unix()%10000)
	}
	if p.Date == "" {
		p.Date = time.Now().Format("2006-01-02")
	}
	if p.PayMode == "" {
		p.PayMode = "Bank Transfer"
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}

	_, err := DB.Exec(`INSERT INTO customer_payments 
		(id, receipt_no, date, customer_name, customer_phone, amount, pay_mode, bank_name, ref_no, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			receipt_no = excluded.receipt_no,
			date = excluded.date,
			customer_name = excluded.customer_name,
			customer_phone = excluded.customer_phone,
			amount = excluded.amount,
			pay_mode = excluded.pay_mode,
			bank_name = excluded.bank_name,
			ref_no = excluded.ref_no,
			note = excluded.note`,
		p.ID, p.ReceiptNo, p.Date, name, phone, p.Amount, p.PayMode, strings.TrimSpace(p.BankName), strings.TrimSpace(p.RefNo), strings.TrimSpace(p.Note), p.CreatedAt,
	)
	if err != nil {
		return p, err
	}

	// Auto-save/update customer in customers table
	if phone != "" || (name != "" && !strings.EqualFold(name, "Walk-in")) {
		_ = saveCustomerLocked(Customer{
			Name:  name,
			Phone: phone,
		})
	}
	return p, nil
}

// DeleteCustomerPayment removes a recorded payment receipt.
func DeleteCustomerPayment(id string) error {
	_, err := DB.Exec("DELETE FROM customer_payments WHERE id = ?", id)
	return err
}

// GetCustomerPayments returns all recorded receipts for a customer.
func GetCustomerPayments(query string) ([]CustomerPayment, error) {
	query = strings.TrimSpace(query)
	var rows *sql.Rows
	var err error
	if query == "" {
		rows, err = DB.Query("SELECT id, receipt_no, date, customer_name, customer_phone, amount, pay_mode, bank_name, ref_no, note, created_at FROM customer_payments ORDER BY date DESC, id DESC LIMIT 100")
	} else {
		rows, err = DB.Query(`SELECT id, receipt_no, date, customer_name, customer_phone, amount, pay_mode, bank_name, ref_no, note, created_at 
			FROM customer_payments 
			WHERE (customer_phone = ? AND customer_phone != '') OR customer_name = ?
			ORDER BY date DESC, id DESC`, query, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CustomerPayment
	for rows.Next() {
		var p CustomerPayment
		if err := rows.Scan(&p.ID, &p.ReceiptNo, &p.Date, &p.CustomerName, &p.CustomerPhone, &p.Amount, &p.PayMode, &p.BankName, &p.RefNo, &p.Note, &p.CreatedAt); err == nil {
			list = append(list, p)
		}
	}
	return list, nil
}

// GetCustomerLedger compiles the complete statement of account (Ledger) matching standard commercial formats.
// Produces: Date | Vch No | Particulars | Debit | Credit | Balance, along with Opening & Closing balance.
func GetCustomerLedger(query string, from string, to string) (*CustomerLedger, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("customer identifier is required")
	}

	// 1. Resolve Customer details
	var c Customer
	err := DB.QueryRow(`
		SELECT id, name, COALESCE(phone, ''), COALESCE(address, ''), COALESCE(gstin, ''), COALESCE(state, 'Tamil Nadu')
		FROM customers 
		WHERE phone = ? OR name = ? OR id = ?
		LIMIT 1`, query, query, query).
		Scan(&c.ID, &c.Name, &c.Phone, &c.Address, &c.Gstin, &c.State)

	if err != nil {
		// Fallback to bills
		var bName, bPhone, bGstin, bAddr, bState sql.NullString
		bErr := DB.QueryRow(`
			SELECT customer_name, customer_phone, customer_gstin, customer_address, customer_state
			FROM bills 
			WHERE customer_phone = ? OR customer_name = ?
			ORDER BY date DESC LIMIT 1`, query, query).
			Scan(&bName, &bPhone, &bGstin, &bAddr, &bState)
		if bErr != nil {
			return nil, errors.New("customer not found")
		}
		c = Customer{
			ID:      GenID("cust"),
			Name:    bName.String,
			Phone:   bPhone.String,
			Address: bAddr.String,
			Gstin:   bGstin.String,
			State:   bState.String,
		}
	}

	ledger := &CustomerLedger{
		Customer: c,
		FromDate: from,
		ToDate:   to,
		Entries:  []CustomerLedgerEntry{},
	}

	phone := c.Phone
	name := c.Name

	// 2. Compute Opening Balance (All debits and credits prior to 'from')
	var openingBal float64
	if from != "" {
		var priorDebits sql.NullFloat64
		var priorBillPaid sql.NullFloat64
		_ = DB.QueryRow(`SELECT SUM(grand_total), SUM(paid) FROM bills 
			WHERE ((customer_phone = ? AND customer_phone != '') OR customer_name = ?) AND date < ?`,
			phone, name, from).Scan(&priorDebits, &priorBillPaid)

		var priorDirectPayments sql.NullFloat64
		_ = DB.QueryRow(`SELECT SUM(amount) FROM customer_payments 
			WHERE ((customer_phone = ? AND customer_phone != '') OR customer_name = ?) AND date < ?`,
			phone, name, from).Scan(&priorDirectPayments)

		deb := 0.0
		if priorDebits.Valid {
			deb = priorDebits.Float64
		}
		cred := 0.0
		if priorBillPaid.Valid {
			cred += priorBillPaid.Float64
		}
		if priorDirectPayments.Valid {
			cred += priorDirectPayments.Float64
		}
		openingBal = roundMoney(deb - cred)
	}
	ledger.OpeningBalance = openingBal

	// 3. Collect Bills within [from, to]
	bQuery := `SELECT id, bill_no, date, grand_total, paid, pay_mode, gst_on, note FROM bills 
		WHERE ((customer_phone = ? AND customer_phone != '') OR customer_name = ?)`
	var bArgs []interface{}
	bArgs = append(bArgs, phone, name)
	if from != "" {
		bQuery += " AND date >= ?"
		bArgs = append(bArgs, from)
	}
	if to != "" {
		bQuery += " AND date <= ?"
		bArgs = append(bArgs, to)
	}
	bQuery += " ORDER BY date ASC, bill_no ASC"

	bRows, err := DB.Query(bQuery, bArgs...)
	if err == nil {
		defer bRows.Close()
		for bRows.Next() {
			var bID, bNo, bDate, bPayMode, bNote string
			var bGrandTotal, bPaid float64
			var gstOnInt int
			if err := bRows.Scan(&bID, &bNo, &bDate, &bGrandTotal, &bPaid, &bPayMode, &gstOnInt, &bNote); err == nil {
				// Invoice Debit entry
				particulars := "Sales"
				if gstOnInt == 1 {
					particulars = "Sales (GST Tax Invoice)"
				}
				ledger.Entries = append(ledger.Entries, CustomerLedgerEntry{
					Date:        bDate,
					VchNo:       bNo,
					Particulars: particulars,
					Type:        "sale",
					Debit:       roundMoney(bGrandTotal),
					Credit:      0,
					RefID:       bID,
				})

				// If counter receipt paid directly on this bill, record credit entry
				if bPaid > 0 {
					payLabel := fmt.Sprintf("Receipt (Counter - %s)", bPayMode)
					ledger.Entries = append(ledger.Entries, CustomerLedgerEntry{
						Date:        bDate,
						VchNo:       bNo,
						Particulars: payLabel,
						Type:        "payment",
						Debit:       0,
						Credit:      roundMoney(bPaid),
						RefID:       bID,
					})
				}
			}
		}
	}

	// 4. Collect Customer Payments (Bank receipts, cheques, standalone receipts)
	pQuery := `SELECT id, receipt_no, date, amount, pay_mode, bank_name, ref_no, note FROM customer_payments 
		WHERE ((customer_phone = ? AND customer_phone != '') OR customer_name = ?)`
	var pArgs []interface{}
	pArgs = append(pArgs, phone, name)
	if from != "" {
		pQuery += " AND date >= ?"
		pArgs = append(pArgs, from)
	}
	if to != "" {
		pQuery += " AND date <= ?"
		pArgs = append(pArgs, to)
	}
	pQuery += " ORDER BY date ASC, receipt_no ASC"

	pRows, err := DB.Query(pQuery, pArgs...)
	if err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var pID, pRecNo, pDate, pPayMode, pBank, pRef, pNote string
			var pAmount float64
			if err := pRows.Scan(&pID, &pRecNo, &pDate, &pAmount, &pPayMode, &pBank, &pRef, &pNote); err == nil {
				part := pBank
				if part == "" {
					part = pPayMode
				}
				if pRef != "" {
					part += fmt.Sprintf("(%s)", pRef)
				}
				if pNote != "" {
					part += " - " + pNote
				}
				ledger.Entries = append(ledger.Entries, CustomerLedgerEntry{
					Date:        pDate,
					VchNo:       pRecNo,
					Particulars: part,
					Type:        "payment",
					Debit:       0,
					Credit:      roundMoney(pAmount),
					RefID:       pID,
				})
			}
		}
	}

	// 5. Stable Sort chronologically by Date. On same date, Debits (sales) come before Credits (payments)
	sort.SliceStable(ledger.Entries, func(i, j int) bool {
		if ledger.Entries[i].Date != ledger.Entries[j].Date {
			return ledger.Entries[i].Date < ledger.Entries[j].Date
		}
		// On the same date, place Sales (Debit) before Receipts (Credit)
		if ledger.Entries[i].Debit > 0 && ledger.Entries[j].Credit > 0 {
			return true
		}
		if ledger.Entries[i].Credit > 0 && ledger.Entries[j].Debit > 0 {
			return false
		}
		return ledger.Entries[i].VchNo < ledger.Entries[j].VchNo
	})

	// 6. Calculate cumulative running balance and totals
	runningBal := openingBal
	var totalDebit, totalCredit float64
	for i := range ledger.Entries {
		totalDebit += ledger.Entries[i].Debit
		totalCredit += ledger.Entries[i].Credit
		runningBal = roundMoney(runningBal + ledger.Entries[i].Debit - ledger.Entries[i].Credit)
		ledger.Entries[i].Balance = runningBal
	}

	ledger.TotalDebit = roundMoney(totalDebit)
	ledger.TotalCredit = roundMoney(totalCredit)
	ledger.ClosingBalance = roundMoney(openingBal + totalDebit - totalCredit)

	if ledger.ClosingBalance > 0.005 {
		ledger.BalanceType = "Debit Balance"
	} else if ledger.ClosingBalance < -0.005 {
		ledger.BalanceType = "Credit Balance"
	} else {
		ledger.BalanceType = "Settled (Nil)"
	}

	return ledger, nil
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
			 subtotal, discount, taxable, gst_on, gst_amt, cgst, sgst, igst, grand_total, pay_mode, paid, balance, cash_paid, upi_paid, note, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
				cash_paid = excluded.cash_paid,
				upi_paid = excluded.upi_paid,
				note = excluded.note`,
			b.ID, b.BillNo, b.Date, b.Customer.Name, b.Customer.Phone, b.Customer.Address, b.Customer.Gstin, b.Customer.State,
			b.Subtotal, b.Discount, b.Taxable, gstOnInt, b.GSTAmt, b.CGST, b.SGST, b.IGST, b.GrandTotal, b.PayMode, b.Paid, b.Balance, b.CashPaid, b.UPIPaid, b.Note, b.CreatedAt,
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
