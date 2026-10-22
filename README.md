# Siraazhi Pro POS — Vanur Farmer Producer Company Limited

## Universal Billing, Multi-Printer, Inventory & GST Suite

Standalone,single-executable point-of-sale software engineered for **Vanur Farmer Producer Company Ltd (VFPC)**. Works 100% offline on Windows with embedded SQLite and zero external runtime dependencies.

📖 **Official Documentation & Operations Guide:**
- 📘 [**Complete User Manual (Markdown)**](docs/USER_MANUAL.md) — Step-by-step instructions with 12 high-resolution screenshots.
- 🌐 [**Interactive / Printable Manual (HTML)**](docs/USER_MANUAL.html) — Double-click to open in Microsoft Edge or Chrome with full illustrations and print-ready CSS.

---

## 🌟 Key Architecture & Highlights

- **Single Portable Executable (`siraazhi-pos.exe`, ~11 MB):**
  - Zero prerequisites: No Node.js, no Python, no database setup, no installer wizard required.
  - Double-click to run on any standard Windows 10/11 PC.
- **Master System vs Client Staff Terminal Mode:**
  - **Master System (`http://localhost:8080`):** Full access to POS Billing, Stock & Inventory Catalog, Batch Crushing, Procurement, Overhead Expenses, Invoices Register & Cancellations, GSTR-1 CA Reports, Multi-Format Exports (Excel/CSV/PDF), and Business Profile Settings.
  - **Client Mobile Terminal (`http://<ip>:8080`):** Staff on smartphones, tablets, or secondary laptops on the shop Wi-Fi have **restricted access to the billing terminal only**. They can scan barcodes, add items, and create bills, but cannot access inventory, delete bills, view company expenses, or export financial data.
  - **Master PIN Unlock (`1234`):** The business owner can unlock Master Mode on any device by entering the Master PIN.
- **Universal Multi-Printer Engine:**
  - **80mm Thermal Receipt (Standard 3-inch POS Roll):** 48-col format with clean tax splits, FSSAI, GSTIN, and company branding.
  - **58mm Thermal Receipt (Portable 2-inch Bluetooth/Handheld):** 32-col ultracompact slip.
  - **A4 Corporate Tax Invoice:** Full-page official invoice with company letterhead, HSN tax summary table, bank details, and signature blocks.
  - **A5 Delivery Slip:** Half-page compact slip.
- **Native PDF Generator:**
  - One-click vector PDF saving for instant WhatsApp or email billing without requiring a physical printer.
- **Multi-Format Financial Exports:**
  - **Excel (.xlsx):** Multi-tab formatted workbooks (Sales Register, Item-Wise Volume/Margin Breakdown).
  - **GSTR-1 Ready Excel:** Formatted for direct CA filing (Table 4 B2B, Table 7 B2C Small, Table 12 HSN Summary).
  - **CSV:** Raw transaction stream for direct Tally import.
  - **JSON:** One-click full database backup and disaster recovery restore.

---

## 🚀 How to Run

### Method 1: Interactive Desktop (Double-Click)

Simply double-click `siraazhi-pos.exe`.
It starts the server on port `8080` and automatically opens your web browser to the POS counter screen.

### Method 2: Silent Background Service (No Black Console Window)

Double-click `start_silent.vbs`.
It launches `siraazhi-pos.exe` in the background with zero visible windows, running continuously on `http://localhost:8080` and `http://<your-ip>:8080`.

### Method 3: Command-Line Flags

```powershell
# Run on default port 8080 and auto-open browser
./siraazhi-pos.exe

# Run on custom port (e.g. 5050)
./siraazhi-pos.exe -port 5050

# Run in background (headless) without opening browser
./siraazhi-pos.exe -headless
```

---

## 📱 Mobile & Tablet Shop Floor Access

1. Ensure the smartphone or tablet is connected to the same Wi-Fi router as the main PC.
2. In the POS screen, click **"📱 Mobile / LAN Access"** in the top header.
3. Scan the displayed QR Code with any phone camera, or type `http://<main-pc-ip>:8080` into the mobile browser.
4. **Install as App:** In the mobile browser menu, tap **"Add to Home Screen"** to install Siraazhi POS as a full-screen, native-feeling mobile app.

---

## ⌨️ Counter POS Keyboard Shortcuts

- `F2`: Focus Cart / New Transaction
- `F3`: Focus Barcode Scanner / Product Search
- `F4`: Toggle GST Tax Invoice vs Cash Bill
- `F10`: Save & Print (routes to default configured printer)
- `Esc`: Clear Cart / Dismiss Modals

---

## 📦 Building from Source (Go)

```powershell
# Tidy Go dependencies
go mod tidy

# Compile standalone executable with symbols stripped
go build -v -ldflags "-s -w" -o siraazhi-pos.exe .
```

*Data is stored safely in `data/pos.db` with WAL mode enabled. Keep weekly backups by downloading the JSON file from the Settings tab.*
