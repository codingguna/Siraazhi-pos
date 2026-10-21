//go:build windows
// +build windows

/* ==========================================================================
   Package printer — Native Windows Print Spooler Driver (Zero-Dialog)
   
   WHAT IT IS:
     Direct Win32 Print Spooler interface using Windows winspool.drv.
     Sends raw byte streams directly to any installed physical USB or shared printer.
     
   HOW IT WORKS:
     1. Uses standard Windows OpenPrinterW, StartDocPrinterW (pDatatype="RAW"),
        StartPagePrinter, WritePrinter, and EndDocPrinterW.
     2. Zero dialogs, zero modals, zero user prompts.
     3. Enumerates all installed printers on the host system.
   ========================================================================== */

package printer

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modwinspool           = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinterW      = modwinspool.NewProc("OpenPrinterW")
	procStartDocPrinterW  = modwinspool.NewProc("StartDocPrinterW")
	procStartPagePrinter  = modwinspool.NewProc("StartPagePrinter")
	procWritePrinter      = modwinspool.NewProc("WritePrinter")
	procEndPagePrinter    = modwinspool.NewProc("EndPagePrinter")
	procEndDocPrinter     = modwinspool.NewProc("EndDocPrinter")
	procClosePrinter      = modwinspool.NewProc("ClosePrinter")
	procEnumPrintersW     = modwinspool.NewProc("EnumPrintersW")
)

const (
	PRINTER_ENUM_LOCAL       = 0x00000002
	PRINTER_ENUM_CONNECTIONS = 0x00000004
)

type printerInfo4 struct {
	pPrinterName *uint16
	pServerName  *uint16
	attributes   uint32
}

// GetInstalledPrinters returns the names of all printers configured on this Windows machine
func GetInstalledPrinters() ([]string, error) {
	var needed, returned uint32
	flags := uintptr(PRINTER_ENUM_LOCAL | PRINTER_ENUM_CONNECTIONS)
	level := uintptr(4)

	// First call to determine buffer size needed
	procEnumPrintersW.Call(
		flags,
		0,
		level,
		0,
		0,
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)

	if needed > 0 {
		buf := make([]byte, needed)
		r1, _, _ := procEnumPrintersW.Call(
			flags,
			0,
			level,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(needed),
			uintptr(unsafe.Pointer(&needed)),
			uintptr(unsafe.Pointer(&returned)),
		)

		if r1 != 0 && returned > 0 {
			printers := make([]string, 0, returned)
			infoSize := unsafe.Sizeof(printerInfo4{})
			for i := uint32(0); i < returned; i++ {
				pInfo := (*printerInfo4)(unsafe.Pointer(&buf[uintptr(i)*infoSize]))
				if pInfo.pPrinterName != nil {
					name := syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(pInfo.pPrinterName))[:])
					if strings.TrimSpace(name) != "" {
						printers = append(printers, strings.TrimSpace(name))
					}
				}
			}
			if len(printers) > 0 {
				return printers, nil
			}
		}
	}

	// Fallback using PowerShell if Win32 EnumPrinters returns empty
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", "Get-CimInstance Win32_Printer | Select-Object -ExpandProperty Name")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		lines := strings.Split(out.String(), "\n")
		var printers []string
		for _, l := range lines {
			name := strings.TrimSpace(l)
			if name != "" {
				printers = append(printers, name)
			}
		}
		if len(printers) > 0 {
			return printers, nil
		}
	}

	return []string{}, nil
}

// SendRawToSpooler sends raw bytes directly to a Windows printer queue without any UI dialog
func SendRawToSpooler(printerName string, docName string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("no print data provided")
	}

	pPrinterName, err := syscall.UTF16PtrFromString(printerName)
	if err != nil {
		return fmt.Errorf("invalid printer name: %w", err)
	}

	var hPrinter uintptr
	r1, _, err := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(pPrinterName)),
		uintptr(unsafe.Pointer(&hPrinter)),
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("could not open printer %q: %v", printerName, err)
	}
	defer procClosePrinter.Call(hPrinter)

	docNamePtr, _ := syscall.UTF16PtrFromString(docName)
	dataTypePtr, _ := syscall.UTF16PtrFromString("RAW")

	docInfo := struct {
		pDocName    *uint16
		pOutputFile *uint16
		pDatatype   *uint16
	}{
		pDocName:  docNamePtr,
		pDatatype: dataTypePtr,
	}

	jobId, _, err := procStartDocPrinterW.Call(
		hPrinter,
		1,
		uintptr(unsafe.Pointer(&docInfo)),
	)
	if jobId == 0 {
		return fmt.Errorf("StartDocPrinter failed: %v", err)
	}
	defer procEndDocPrinter.Call(hPrinter)

	r1, _, err = procStartPagePrinter.Call(hPrinter)
	if r1 == 0 {
		return fmt.Errorf("StartPagePrinter failed: %v", err)
	}
	defer procEndPagePrinter.Call(hPrinter)

	var written uint32
	r1, _, err = procWritePrinter.Call(
		hPrinter,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
		uintptr(unsafe.Pointer(&written)),
	)
	if r1 == 0 {
		return fmt.Errorf("WritePrinter failed: %v", err)
	}

	return nil
}
