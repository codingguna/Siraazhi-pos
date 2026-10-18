/* ==========================================================================
   Siraazhi Pro POS — Main Application Entrypoint & Native Windows Window Mode
   
   WHAT IT IS:
     The central controller of the Siraazhi Pro POS executable:
     - Parses command-line flags (-port, -headless, -db).
     - Initializes the embedded SQLite database engine (zero CGO, zero DLLs).
     - Binds the complete REST API and static embedded web assets.
     - Launches a DEDICATED NATIVE DESKTOP WINDOW (frameless, no address bar,
       no browser tabs, real desktop application feel with taskbar integration).
     - Supports running as a silent background Windows Service for mobile/tablet terminals.

   HOW IT WORKS:
     1. Resolves binary directory so data/pos.db is always stored portably.
     2. Runs database migrations and seeds initial products (Coconut, Groundnut, Sesame).
     3. Configures standard Go http.ServeMux with CORS and Master/Staff role security.
     4. Discovers local network IPs to display connect links for phones/tablets.
     5. Launches the POS in a Native Windows App Window using Windows Edge/Chrome
        App Mode (--app=http://localhost:8080).
     6. Starts the HTTP listener on port 8080 (or custom -port).

   DATA FLOW:
     Main Entry -> DB Init -> Route Registration -> Native Desktop App Window Launch -> HTTP Listen & Serve
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
	"strings"
	"time"

	"siraazhi-pos/db"
	"siraazhi-pos/handlers"
	"siraazhi-pos/web"
)

func isServerRunning(port int) bool {
	client := http.Client{
		Timeout: 400 * time.Millisecond,
	}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/settings", port))
	if err == nil {
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
	// Fallback TCP dial check
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 250*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

func main() {
	// 1. Command-line flags
	port := flag.Int("port", 8080, "HTTP server port (default 8080)")
	headless := flag.Bool("headless", false, "Run in background without opening desktop window")
	dbPath := flag.String("db", "", "Path to SQLite database file (default data/pos.db)")
	flag.Parse()

	localURL := fmt.Sprintf("http://localhost:%d", *port)

	// 2. Single-Instance Check:
	// If the server is already active on this port (e.g. background service running or shortcut re-clicked),
	// DO NOT fail with 'bind: address already in use'.
	// Instead, simply bring up / reopen the Native Desktop Window and exit the secondary launcher process!
	if isServerRunning(*port) {
		if !*headless {
			openNativeAppWindow(localURL)
			time.Sleep(500 * time.Millisecond)
		}
		return
	}

	// 3. Resolve portable data directory relative to executable
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

	mux.HandleFunc("/api/customers", handlers.HandleCustomers)

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
	fmt.Printf("\n [Master System] Active on: %s\n", localURL)
	fmt.Println(" [Mobile Access] Shop Floor & Tablets on Wi-Fi can connect via:")

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			name := strings.ToLower(iface.Name)
			isWiFi := strings.Contains(name, "wi-fi") || strings.Contains(name, "wifi") || strings.Contains(name, "wlan")
			isEthernet := (strings.Contains(name, "ethernet") || strings.Contains(name, "eth")) && !strings.Contains(name, "vethernet")

			if strings.Contains(name, "virtual") || strings.Contains(name, "vethernet") ||
				strings.Contains(name, "wsl") || strings.Contains(name, "bluetooth") || strings.Contains(name, "*") {
				continue
			}
			if !isWiFi && !isEthernet {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
					if ip4 := ipNet.IP.To4(); ip4 != nil && !strings.HasPrefix(ip4.String(), "169.254.") {
						fmt.Printf("   -> http://%s:%d  (Staff Terminal via %s)\n", ip4.String(), *port, iface.Name)
					}
				}
			}
		}
	}
	fmt.Println("==================================================================")

	// 6. Automatically open dedicated Native Desktop Window on host PC if not running headless
	if !*headless {
		go func() {
			time.Sleep(600 * time.Millisecond)
			openNativeAppWindow(localURL)
		}()
	}

	// 7. Start HTTP Server
	serverAddr := ":" + strconv.Itoa(*port)
	if err := http.ListenAndServe(serverAddr, rootHandler); err != nil {
		log.Fatalf("Server terminated: %v", err)
	}
}

// openNativeAppWindow launches the POS in a dedicated native desktop application window.
//
// WHAT IT DOES:
//   Opens a frameless, standalone desktop window without browser tabs, URL bar, or navigation buttons.
//
// HOW IT WORKS:
//   1. Uses Microsoft Edge or Google Chrome Application Mode (--app=http://localhost:8080).
//   2. This runs inside the system's native WebView engine with custom window geometry (1280x820).
//   3. Sits on the Windows Taskbar with its own application icon.
//   4. Falls back to standard browser protocol handler if neither browser is found.
func openNativeAppWindow(url string) {
	if runtime.GOOS == "windows" {
		// Try Microsoft Edge Application Mode (Standard on all Windows 10 & 11 PCs)
		edgePaths := []string{
			os.Getenv("ProgramFiles(x86)") + `\Microsoft\Edge\Application\msedge.exe`,
			os.Getenv("ProgramFiles") + `\Microsoft\Edge\Application\msedge.exe`,
		}
		for _, path := range edgePaths {
			if _, err := os.Stat(path); err == nil {
				cmd := exec.Command(path, fmt.Sprintf("--app=%s", url), "--window-size=1280,820")
				if err := cmd.Start(); err == nil {
					return
				}
			}
		}

		// Try Google Chrome Application Mode
		chromePaths := []string{
			os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("ProgramFiles(x86)") + `\Google\Chrome\Application\chrome.exe`,
			os.Getenv("LocalAppData") + `\Google\Chrome\Application\chrome.exe`,
		}
		for _, path := range chromePaths {
			if _, err := os.Stat(path); err == nil {
				cmd := exec.Command(path, fmt.Sprintf("--app=%s", url), "--window-size=1280,820")
				if err := cmd.Start(); err == nil {
					return
				}
			}
		}
	}

	// Fallback to default browser
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
