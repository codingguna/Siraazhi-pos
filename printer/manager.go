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
	"strings"

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
	return s.PrinterInterface != "browser"
}

// PrintBillToHardware formats and sends the bill directly to the configured physical printer(s).
// Returns PrintResult indicating whether it was directly printed or requires browser dialog.
func PrintBillToHardware(bill *db.Bill, format string, s *db.Settings) PrintResult {
	if s == nil {
		return PrintResult{Success: false, Message: "Settings not available"}
	}

	// If explicitly set to manual browser dialog, return indicating so
	if s.PrinterInterface == "browser" {
		return PrintResult{
			Success:     true,
			DirectPrint: false,
			Message:     "Manual browser print dialog active",
		}
	}

	targetPrinter := s.PrinterTarget
	if targetPrinter == "" && s.PrinterInterface != "network" {
		if def, err := GetDefaultPrinterName(); err == nil && def != "" {
			targetPrinter = def
		}
	}

	if targetPrinter == "" {
		return PrintResult{
			Success: false,
			Message: "No printer found or configured. Please set your printer in Settings.",
		}
	}

	// Build raw ESC/POS or GDI byte sequence
	copies := s.PrintCopies
	if copies < 1 {
		copies = 1
	}

	docName := fmt.Sprintf("Bill_%s", bill.BillNo)

	// Determine effective format
	effectiveFormat := strings.ToLower(strings.TrimSpace(format))
	if effectiveFormat == "" || effectiveFormat == "default" {
		if s.DefaultPrinter != "" {
			effectiveFormat = strings.ToLower(strings.TrimSpace(s.DefaultPrinter))
		} else if !IsThermalPOSPrinter(targetPrinter) || s.PrinterType == "document" {
			effectiveFormat = "a4"
		} else {
			effectiveFormat = "80mm"
		}
	}

	// Determine whether primary target is Thermal POS or Document (A4/Deskjet/HP Smart Tank)
	isThermal := false
	if s.PrinterInterface == "network" {
		// LAN raw socket printer (port 9100)
		isThermal = true
	} else if !IsThermalPOSPrinter(targetPrinter) {
		// Non-thermal desktop office printer (HP Smart Tank, Deskjet, LaserJet, etc.)
		// MUST ALWAYS use Windows GDI Document printing! Never send raw ESC/POS!
		isThermal = false
	} else if s.PrinterType == "document" {
		isThermal = false
	} else if s.PrinterType == "thermal" {
		isThermal = true
	} else {
		// Auto-detect based on printer driver and requested format
		isThermal = IsThermalPOSPrinter(targetPrinter) && effectiveFormat != "a4" && effectiveFormat != "a5" && effectiveFormat != "letter"
	}

	var primaryErr error

	if isThermal {
		// 1. Thermal POS Receipt Printer (80mm / 58mm Roll — Store Default & Preference)
		paperSize := s.ThermalPaperSize
		if paperSize == "" {
			paperSize = s.PaperSize
		}
		if effectiveFormat == "58mm" || effectiveFormat == "80mm" {
			paperSize = effectiveFormat
		}
		if paperSize != "58mm" {
			paperSize = "80mm" // Default 80mm roll
		}

		for c := 0; c < copies; c++ {
			data := BuildReceiptESC(bill, s, paperSize, s.AutoCut, s.OpenCashDrawer)
			if s.PrinterInterface == "network" {
				port := s.PrinterPort
				if port <= 0 {
					port = 9100
				}
				primaryErr = SendRawToNetwork(targetPrinter, port, data)
			} else {
				// Windows Spooler (USB / Driver) - pure Win32 API, zero console window!
				primaryErr = SendRawToSpooler(targetPrinter, docName, data)
			}

			if primaryErr != nil {
				break
			}
		}
	} else {
		// 2. Standard Office / Desktop Printer (HP Smart Tank, LaserJet, Inkjet, Microsoft Print to PDF)
		paperSize := effectiveFormat
		if paperSize == "" || paperSize == "default" {
			paperSize = s.PaperSize
			if paperSize == "" {
				paperSize = s.DefaultPrinter
			}
		}
		if paperSize == "" {
			paperSize = "80mm"
		}

		orientation := s.Orientation
		if orientation == "" {
			orientation = "portrait"
		}

		colorMode := s.ColorMode
		if colorMode == "" {
			colorMode = "color"
		}

		primaryErr = PrintBillGDI(targetPrinter, docName, bill, s, paperSize, orientation, colorMode, copies)
	}

	if primaryErr != nil {
		log.Printf("[Printer] Primary print failed for %s on %q: %v", bill.BillNo, targetPrinter, primaryErr)
		return PrintResult{
			Success:     false,
			DirectPrint: true,
			PrinterUsed: targetPrinter,
			Error:       primaryErr.Error(),
			Message:     fmt.Sprintf("Failed to print on %s: %v", targetPrinter, primaryErr),
		}
	}

	log.Printf("[Printer] Successfully printed Bill %s to %q (Copies: %d, Thermal: %v)", bill.BillNo, targetPrinter, copies, isThermal)

	// Handle Optional Secondary Printer (e.g. Dispatch / Packaging / Warehouse counter)
	if s.SecondaryPrinterEnabled && s.SecondaryPrinterTarget != "" {
		go func() {
			secIsThermal := false
			if s.SecondaryPrinterInterface == "network" {
				secIsThermal = true
			} else if !IsThermalPOSPrinter(s.SecondaryPrinterTarget) {
				secIsThermal = false
			} else if s.SecondaryPrinterType == "document" {
				secIsThermal = false
			} else if s.SecondaryPrinterType == "thermal" {
				secIsThermal = true
			} else {
				secIsThermal = IsThermalPOSPrinter(s.SecondaryPrinterTarget)
			}

			var secErr error
			if secIsThermal {
				secPaper := s.SecondaryPaperSize
				if secPaper != "58mm" {
					secPaper = "80mm"
				}
				secData := BuildReceiptESC(bill, s, secPaper, s.AutoCut, false)
				if s.SecondaryPrinterInterface == "network" {
					secPort := s.SecondaryPrinterPort
					if secPort <= 0 {
						secPort = 9100
					}
					secErr = SendRawToNetwork(s.SecondaryPrinterTarget, secPort, secData)
				} else {
					secErr = SendRawToSpooler(s.SecondaryPrinterTarget, docName+"_Copy", secData)
				}
			} else {
				secPaper := s.SecondaryPaperSize
				if secPaper == "" {
					secPaper = "80mm"
				}
				secErr = PrintBillGDI(s.SecondaryPrinterTarget, docName+"_Copy", bill, s, secPaper, "portrait", "color", 1)
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
		PrinterUsed: targetPrinter,
		Message:     fmt.Sprintf("Bill %s printed directly to %s", bill.BillNo, targetPrinter),
	}
}

// PrintTestSlip sends a test receipt ticket or document to the specified target
func PrintTestSlip(intf, target string, port int, paperSize, orientation, colorMode, printerType string, s *db.Settings) error {
	if intf == "" {
		intf = "spooler"
	}
	if target == "" && intf != "network" {
		if def, err := GetDefaultPrinterName(); err == nil && def != "" {
			target = def
		}
	}
	if target == "" {
		return fmt.Errorf("no printer configured or detected")
	}
	if paperSize == "" {
		paperSize = "80mm"
	}

	if intf == "network" {
		if port <= 0 {
			port = 9100
		}
		testData := BuildTestReceipt(target, paperSize, s)
		return SendRawToNetwork(target, port, testData)
	}

	// Windows Spooler Interface:
	// Check if this target is a Thermal POS printer or a standard Document printer (HP Smart Tank, etc.)
	isThermal := false
	if !IsThermalPOSPrinter(target) {
		// Non-thermal desktop printer - NEVER send raw ESC/POS
		isThermal = false
	} else if printerType == "thermal" {
		isThermal = true
	} else if printerType == "document" {
		isThermal = false
	} else {
		// Auto-detect based on printer name / driver
		isThermal = IsThermalPOSPrinter(target)
	}

	if isThermal {
		// Thermal POS ESC/POS receipt with cut (100% Win32 C-API spooler, ZERO terminal window)
		if paperSize != "58mm" {
			paperSize = "80mm"
		}
		testData := BuildTestReceipt(target, paperSize, s)
		return SendRawToSpooler(target, "POS_Test_Slip", testData)
	}

	// Standard Office / Desktop Printer (HP Smart Tank, LaserJet, Inkjet, Microsoft Print to PDF)
	if paperSize == "" {
		paperSize = "80mm"
	}
	if orientation == "" {
		orientation = "portrait"
	}
	if colorMode == "" {
		colorMode = "color"
	}

	return PrintTestSlipGDI(target, paperSize, orientation, colorMode, s)
}

