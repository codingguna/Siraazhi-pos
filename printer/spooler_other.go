//go:build !windows
// +build !windows

package printer

import (
	"fmt"
)

// GetInstalledPrinters fallback for non-Windows platforms
func GetInstalledPrinters() ([]string, error) {
	return []string{"Default Printer (Non-Windows)"}, nil
}

// SendRawToSpooler fallback for non-Windows platforms
func SendRawToSpooler(printerName string, docName string, data []byte) error {
	return fmt.Errorf("Windows spooler is only available on Windows host")
}

// PrintDocumentWin32 fallback for non-Windows platforms
func PrintDocumentWin32(printerName, docName, content, paperSize, orientation, colorMode string, copies int) error {
	return fmt.Errorf("Windows document printing is only available on Windows host")
}

// IsThermalPOSPrinter fallback for non-Windows platforms
func IsThermalPOSPrinter(name string) bool {
	return true
}

// GetDefaultPrinterName fallback for non-Windows platforms
func GetDefaultPrinterName() (string, error) {
	return "", nil
}

