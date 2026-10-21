/* ==========================================================================
   Package printer — Master Hardware Print Dispatcher & Multi-Printer Coordinator
   
   WHAT IT IS:
     Central coordinator for all physical printers attached to Siraazhi POS:
     - Dispatches thermal receipts to primary counter printer (USB/Spooler or LAN TCP).
     - Dispatches duplicate slips to secondary dispatch/kitchen/packaging printers.
     - Supports unattended zero-dialog mobile order billing.
     - Provides verification test slip execution.
   ========================================================================== */

package printer

import (
	"fmt"
	"log"

	"siraazhi-pos/db"
)

// PrintResult encapsulates status of a hardware print attempt
type PrintResult struct {
	Success      bool   `json:"success"`
	DirectPrint  bool   `json:"directPrint"`
	PrinterUsed  string `json:"printerUsed,omitempty"`
	Message      string `json:"message"`
	Error        string `json:"error,omitempty"`
}

// IsHardwarePrintingActive checks if automatic hardware direct printing is configured
func IsHardwarePrintingActive(s *db.Settings) bool {
	if s == nil {
		return false
	}
	intf := s.PrinterInterface
	return (intf == "spooler" || intf == "network") && s.PrinterTarget != ""
}

// PrintBillToHardware formats and sends the bill directly to the configured physical printer(s).
// Returns PrintResult indicating whether it was directly printed or requires browser dialog.
func PrintBillToHardware(bill *db.Bill, format string, s *db.Settings) PrintResult {
	if s == nil {
		return PrintResult{Success: false, Message: "Settings not available"}
	}

	// If configured as manual browser print, let the browser window.print() handle it
	if !IsHardwarePrintingActive(s) {
		return PrintResult{
			Success:     true,
			DirectPrint: false,
			Message:     "Manual browser print dialog active",
		}
	}

	paperSize := s.ThermalPaperSize
	if paperSize == "" {
		paperSize = s.DefaultPrinter
	}
	if format == "58mm" || format == "80mm" {
		paperSize = format
	}
	if paperSize != "58mm" {
		paperSize = "80mm" // Default standard size
	}

	// Build raw ESC/POS byte sequence
	copies := s.PrintCopies
	if copies < 1 {
		copies = 1
	}

	docName := fmt.Sprintf("Bill_%s", bill.BillNo)

	// Send to Primary Printer
	var primaryErr error
	for c := 0; c < copies; c++ {
		data := BuildReceiptESC(bill, s, paperSize, s.AutoCut, s.OpenCashDrawer)
		if s.PrinterInterface == "network" {
			port := s.PrinterPort
			if port <= 0 {
				port = 9100
			}
			primaryErr = SendRawToNetwork(s.PrinterTarget, port, data)
		} else {
			// Windows Spooler (USB / Driver)
			primaryErr = SendRawToSpooler(s.PrinterTarget, docName, data)
		}

		if primaryErr != nil {
			break
		}
	}

	if primaryErr != nil {
		log.Printf("[Printer] Primary print failed for %s on %q: %v", bill.BillNo, s.PrinterTarget, primaryErr)
		return PrintResult{
			Success:     false,
			DirectPrint: true,
			PrinterUsed: s.PrinterTarget,
			Error:       primaryErr.Error(),
			Message:     fmt.Sprintf("Failed to print on %s: %v", s.PrinterTarget, primaryErr),
		}
	}

	log.Printf("[Printer] Successfully printed Bill %s to %q (Copies: %d)", bill.BillNo, s.PrinterTarget, copies)

	// Handle Optional Secondary Printer (e.g. Dispatch / Packaging / Warehouse counter)
	if s.SecondaryPrinterEnabled && s.SecondaryPrinterTarget != "" {
		go func() {
			secData := BuildReceiptESC(bill, s, paperSize, s.AutoCut, false)
			var secErr error
			if s.SecondaryPrinterInterface == "network" {
				secPort := s.SecondaryPrinterPort
				if secPort <= 0 {
					secPort = 9100
				}
				secErr = SendRawToNetwork(s.SecondaryPrinterTarget, secPort, secData)
			} else {
				secErr = SendRawToSpooler(s.SecondaryPrinterTarget, docName+"_Copy", secData)
			}
			if secErr != nil {
				log.Printf("[Printer] Secondary print failed on %q: %v", s.SecondaryPrinterTarget, secErr)
			} else {
				log.Printf("[Printer] Successfully sent copy of Bill %s to secondary %q", bill.BillNo, s.SecondaryPrinterTarget)
			}
		}()
	}

	return PrintResult{
		Success:     true,
		DirectPrint: true,
		PrinterUsed: s.PrinterTarget,
		Message:     fmt.Sprintf("Bill %s printed directly to %s", bill.BillNo, s.PrinterTarget),
	}
}

// PrintTestSlip sends a test receipt ticket to the specified target
func PrintTestSlip(intf, target string, port int, paperSize string, s *db.Settings) error {
	if intf == "" {
		intf = "spooler"
	}
	if paperSize == "" {
		paperSize = "80mm"
	}

	testData := BuildTestReceipt(target, paperSize, s)

	if intf == "network" {
		if port <= 0 {
			port = 9100
		}
		return SendRawToNetwork(target, port, testData)
	}

	return SendRawToSpooler(target, "POS_Test_Slip", testData)
}
