/* ==========================================================================
   Siraazhi Pro POS — Main Application Entrypoint
   
   WHAT IT IS:
     The central controller of the Siraazhi Pro POS executable:
     - Parses command-line flags (-port, -headless, -service).
     - Initializes the embedded SQLite database engine (zero CGO, zero DLLs).
     - Binds the complete REST API and static embedded web assets.
     - Automatically launches the default web browser on Windows desktop startup.
     - Supports running as a silent background Windows Service.

   HOW IT WORKS:
     1. Resolves binary directory so data/pos.db is always stored portably.
     2. Runs database migrations and seeds initial products (Coconut, Groundnut, Sesame).
     3. Configures standard Go http.ServeMux with CORS and Master/Staff role security.
     4. Discovers local network IPs to display connect links for phones/tablets.
     5. Launches the user's browser in a background goroutine.
     6. Starts the HTTP listener on port 8080 (or custom -port).

   DATA FLOW:
     Main Entry -> DB Init -> Route Registration -> Browser Auto-Launch -> HTTP Listen & Serve
   ========================================================================== */

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"siraazhi-pos/db"
	"siraazhi-pos/handlers"
	"siraazhi-pos/web"
)

func main() {
	// 1. Command-line flags
	port := flag.Int("port", 8080, "HTTP server port (default 8080)")
	headless := flag.Bool("headless", false, "Run in background without opening browser")
	dbPath := flag.String("db", "", "Path to SQLite database file (default data/pos.db)")
	flag.Parse()

	// 2. Resolve portable data directory relative to executable
	execDir, err := os.Executable()
	if err != nil {
		execDir = "."
	} else {
		execDir = filepath.Dir(execDir)
	}

	actualDBPath := *dbPath
	if actualDBPath == "" {
		actualDBPath = filepath.Join(execDir, "data", "pos.db")
	}

	fmt.Println("==================================================================")
	fmt.Println("  Siraazhi Pro POS — Vanur Farmer Producer Company Limited")
	fmt.Println("  Enterprise Billing, Multi-Printer, Inventory & GST Suite")
	fmt.Println("==================================================================")
	fmt.Printf(" [Database] Loading SQLite: %s\n", actualDBPath)

	// 3. Initialize SQLite database & auto-migrations
	if err := db.InitDB(actualDBPath); err != nil {
		log.Fatalf("Fatal: Database initialization failed: %v", err)
	}
	fmt.Println(" [Database] SQLite tables & indexes verified ✓")

	// 4. Configure HTTP Router
	mux := http.NewServeMux()

	// REST API Endpoints
	mux.HandleFunc("/api/products", handlers.HandleProducts)
	mux.HandleFunc("/api/products/", handlers.HandleProductByID)
	mux.HandleFunc("/api/products/barcode/", handlers.HandleProductByBarcode)

	mux.HandleFunc("/api/bills", handlers.HandleBills)
	mux.HandleFunc("/api/bills/", handlers.HandleBillByID)

	mux.HandleFunc("/api/inventory/adjust", handlers.HandleStockAdjust)
	mux.HandleFunc("/api/inventory/production", handlers.HandleProduction)
	mux.HandleFunc("/api/inventory/moves", handlers.HandleStockMoves)

	mux.HandleFunc("/api/purchases", handlers.HandlePurchases)
	mux.HandleFunc("/api/purchases/", handlers.HandlePurchaseDelete)

	mux.HandleFunc("/api/expenses", handlers.HandleExpenses)
	mux.HandleFunc("/api/expenses/", handlers.HandleExpenseDelete)

	mux.HandleFunc("/api/settings", handlers.HandleSettings)
	mux.HandleFunc("/api/dashboard", handlers.HandleDashboard)
	mux.HandleFunc("/api/reports/gstr1", handlers.HandleGSTR1Report)

	mux.HandleFunc("/api/export/csv", handlers.HandleSalesCSV)
	mux.HandleFunc("/api/backup", handlers.HandleBackup)
	mux.HandleFunc("/api/restore", handlers.HandleRestore)

	mux.HandleFunc("/api/network-info", handlers.HandleNetworkInfo(*port))

	// Embedded static web frontend (//go:embed static/*)
	mux.Handle("/", web.Handler())

	// Wrap entire router with CORS and Master vs Staff role authorization
	rootHandler := handlers.CORSMiddleware(handlers.MasterRoleMiddleware(mux))

	// 5. Discover Local LAN IP addresses for mobile terminal access
	localURL := fmt.Sprintf("http://localhost:%d", *port)
	fmt.Printf("\n [Master System] Active on: %s\n", localURL)
	fmt.Println(" [Mobile Access] Shop Floor & Tablets on Wi-Fi can connect via:")

	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				if ipNet.IP.To4() != nil {
					fmt.Printf("   -> http://%s:%d  (Staff Billing Terminal)\n", ipNet.IP.String(), *port)
				}
			}
		}
	}
	fmt.Println("==================================================================")

	// 6. Automatically open browser on host PC if not running headless
	if !*headless {
		go func() {
			time.Sleep(600 * time.Millisecond)
			openBrowser(localURL)
		}()
	}

	// 7. Start HTTP Server
	serverAddr := ":" + strconv.Itoa(*port)
	if err := http.ListenAndServe(serverAddr, rootHandler); err != nil {
		log.Fatalf("Server terminated: %v", err)
	}
}

// openBrowser launches the default web browser on Windows/macOS/Linux.
// Flow: identifies runtime OS -> triggers OS shell open command -> user sees POS screen.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Could not auto-launch browser: %v (Open manually at %s)", err, url)
	}
}
