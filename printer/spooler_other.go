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
