//go:build windows
// +build windows

/* ==========================================================================
   Package printer — Native Windows Print Spooler & GDI Driver (Zero-Dialog)
   
   WHAT IT IS:
     100% in-process Win32 Print Spooler and GDI interface using Windows native
     winspool.drv and gdi32.dll.
     
   HOW IT WORKS:
     1. Thermal Roll Printers (80mm / 58mm):
        Uses direct winspool.drv OpenPrinterW, StartDocPrinterW (pDatatype="RAW"),
        StartPagePrinter, WritePrinter, EndPagePrinter, and EndDocPrinterW.
        Zero dialogs, zero modals, zero external processes.
        
     2. Standard Office / Desktop Printers (HP Smart Tank 525, LaserJet, Inkjet):
        Uses direct gdi32.dll CreateDCW, DEVMODE document properties (A4/A5,
        portrait/landscape, color/monochrome), StartDocW, StartPage, TextOutW,
        EndPage, and EndDoc.
        
     CRITICAL GUARANTEE:
       NO subprocesses (no cmd.exe, no powershell.exe) are ever launched.
       Execution is 100% in-process via OS system DLLs. No terminal, console,
       or command prompt window will EVER appear on the user's screen!
   ========================================================================== */

package printer

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modwinspool             = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinterW        = modwinspool.NewProc("OpenPrinterW")
	procStartDocPrinterW    = modwinspool.NewProc("StartDocPrinterW")
	procStartPagePrinter    = modwinspool.NewProc("StartPagePrinter")
	procWritePrinter        = modwinspool.NewProc("WritePrinter")
	procEndPagePrinter      = modwinspool.NewProc("EndPagePrinter")
	procEndDocPrinter       = modwinspool.NewProc("EndDocPrinter")
	procClosePrinter        = modwinspool.NewProc("ClosePrinter")
	procEnumPrintersW       = modwinspool.NewProc("EnumPrintersW")
	procGetDefaultPrinterW  = modwinspool.NewProc("GetDefaultPrinterW")
	procDocumentPropertiesW = modwinspool.NewProc("DocumentPropertiesW")

	modgdi32         = syscall.NewLazyDLL("gdi32.dll")
	procCreateDCW    = modgdi32.NewProc("CreateDCW")
	procDeleteDC     = modgdi32.NewProc("DeleteDC")
	procStartDocW    = modgdi32.NewProc("StartDocW")
	procEndDoc       = modgdi32.NewProc("EndDoc")
	procStartPage    = modgdi32.NewProc("StartPage")
	procEndPage      = modgdi32.NewProc("EndPage")
	procTextOutW     = modgdi32.NewProc("TextOutW")
	procSelectObject = modgdi32.NewProc("SelectObject")
	procDeleteObject = modgdi32.NewProc("DeleteObject")
	procCreateFontW  = modgdi32.NewProc("CreateFontW")
	procGetDeviceCaps= modgdi32.NewProc("GetDeviceCaps")
)

const (
	PRINTER_ENUM_LOCAL       = 0x00000002
	PRINTER_ENUM_CONNECTIONS = 0x00000004

	HORZRES     = 8
	VERTRES     = 10
	LOGPIXELSX  = 88
	LOGPIXELSY  = 90
	FW_NORMAL   = 400
	FW_BOLD     = 700
	ANSI_CHARSET = 0
	OUT_DEFAULT_PRECIS = 0
	CLIP_DEFAULT_PRECIS = 0
	DEFAULT_QUALITY = 0
	DEFAULT_PITCH = 0

	DM_OUT_BUFFER = 2
	DM_IN_BUFFER  = 8

	DM_ORIENTATION = 0x00000001
	DM_PAPERSIZE   = 0x00000002
	DM_COPIES      = 0x00000100
	DM_COLOR       = 0x00000800

	DMORIENT_PORTRAIT  = 1
	DMORIENT_LANDSCAPE = 2

	DMPAPER_LETTER = 1
	DMPAPER_A4     = 9
	DMPAPER_A5     = 11

	DMCOLOR_MONOCHROME = 1
	DMCOLOR_COLOR      = 2
)

type printerInfo4 struct {
	pPrinterName *uint16
	pServerName  *uint16
	attributes   uint32
}

type printerInfo2 struct {
	pServerName         *uint16
	pPrinterName        *uint16
	pShareName          *uint16
	pPortName           *uint16
	pDriverName         *uint16
	pComment            *uint16
	pLocation           *uint16
	pDevMode            uintptr
	pSepFile            *uint16
	pPrintProcessor     *uint16
	pDatatype           *uint16
	pParameters         *uint16
	pSecurityDescriptor uintptr
	attributes          uint32
	priority            uint32
	defaultPriority     uint32
	startTime           uint32
	untilTime           uint32
	status              uint32
	cJobs               uint32
	averagePPM          uint32
}

type DOCINFOW struct {
	cbSize       int32
	lpszDocName  *uint16
	lpszOutput   *uint16
	lpszDatatype *uint16
	fwType       uint32
}

// GetDefaultPrinterName returns the system default printer configured in Windows
func GetDefaultPrinterName() (string, error) {
	var size uint32
	procGetDefaultPrinterW.Call(0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return "", fmt.Errorf("no default printer configured")
	}
	buf := make([]uint16, size)
	r1, _, err := procGetDefaultPrinterW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r1 == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf), nil
}

// GetInstalledPrinters returns the names of all printers configured on this Windows machine using 100% native Win32 EnumPrintersW
func GetInstalledPrinters() ([]string, error) {
	flags := uintptr(PRINTER_ENUM_LOCAL | PRINTER_ENUM_CONNECTIONS)
	seen := make(map[string]bool)
	var printers []string

	// Try Level 4 (Fastest on NT)
	var needed, returned uint32
	procEnumPrintersW.Call(
		flags, 0, 4, 0, 0,
		uintptr(unsafe.Pointer(&needed)),
		uintptr(unsafe.Pointer(&returned)),
	)
	if needed > 0 {
		buf := make([]byte, needed)
		r1, _, _ := procEnumPrintersW.Call(
			flags, 0, 4,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(needed),
			uintptr(unsafe.Pointer(&needed)),
			uintptr(unsafe.Pointer(&returned)),
		)
		if r1 != 0 && returned > 0 {
			infoSize := unsafe.Sizeof(printerInfo4{})
			for i := uint32(0); i < returned; i++ {
				pInfo := (*printerInfo4)(unsafe.Pointer(&buf[uintptr(i)*infoSize]))
				if pInfo.pPrinterName != nil {
					name := syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(pInfo.pPrinterName))[:])
					trimmed := strings.TrimSpace(name)
					if trimmed != "" && !seen[trimmed] {
						seen[trimmed] = true
						printers = append(printers, trimmed)
					}
				}
			}
		}
	}

	// If Level 4 returned nothing, try Level 2
	if len(printers) == 0 {
		needed = 0
		returned = 0
		procEnumPrintersW.Call(
			flags, 0, 2, 0, 0,
			uintptr(unsafe.Pointer(&needed)),
			uintptr(unsafe.Pointer(&returned)),
		)
		if needed > 0 {
			buf := make([]byte, needed)
			r1, _, _ := procEnumPrintersW.Call(
				flags, 0, 2,
				uintptr(unsafe.Pointer(&buf[0])),
				uintptr(needed),
				uintptr(unsafe.Pointer(&needed)),
				uintptr(unsafe.Pointer(&returned)),
			)
			if r1 != 0 && returned > 0 {
				infoSize := unsafe.Sizeof(printerInfo2{})
				for i := uint32(0); i < returned; i++ {
					pInfo := (*printerInfo2)(unsafe.Pointer(&buf[uintptr(i)*infoSize]))
					if pInfo.pPrinterName != nil {
						name := syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(pInfo.pPrinterName))[:])
						trimmed := strings.TrimSpace(name)
						if trimmed != "" && !seen[trimmed] {
							seen[trimmed] = true
							printers = append(printers, trimmed)
						}
					}
				}
			}
		}
	}

	// Always ensure the default printer is in the list
	if def, err := GetDefaultPrinterName(); err == nil && def != "" {
		if !seen[def] {
			printers = append([]string{def}, printers...)
			seen[def] = true
		}
	}

	return printers, nil
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

// createDevMode builds a customized Windows DEVMODE buffer for paper size, orientation, color mode, and copies
func createDevMode(hPrinter uintptr, printerName, paperSize, orientation, colorMode string, copies int) []byte {
	pDevice, _ := syscall.UTF16PtrFromString(printerName)

	needed, _, _ := procDocumentPropertiesW.Call(
		0, hPrinter,
		uintptr(unsafe.Pointer(pDevice)),
		0, 0, 0,
	)
	if int32(needed) <= 0 {
		return nil
	}

	devModeBuf := make([]byte, needed)
	r1, _, _ := procDocumentPropertiesW.Call(
		0, hPrinter,
		uintptr(unsafe.Pointer(pDevice)),
		uintptr(unsafe.Pointer(&devModeBuf[0])),
		0,
		uintptr(DM_OUT_BUFFER),
	)
	if int32(r1) < 0 {
		return nil
	}

	// DEVMODEW layout offsets:
	// dmFields (offset 40, DWORD = 4 bytes)
	// dmOrientation (offset 44, short = 2 bytes)
	// dmPaperSize (offset 46, short = 2 bytes)
	// dmCopies (offset 54, short = 2 bytes)
	// dmColor (offset 60, short = 2 bytes)
	if len(devModeBuf) >= 70 {
		fields := (*uint32)(unsafe.Pointer(&devModeBuf[40]))
		pOrient := (*int16)(unsafe.Pointer(&devModeBuf[44]))
		pPaper := (*int16)(unsafe.Pointer(&devModeBuf[46]))
		pCopies := (*int16)(unsafe.Pointer(&devModeBuf[54]))
		pColor := (*int16)(unsafe.Pointer(&devModeBuf[60]))

		*fields |= DM_COPIES
		if copies < 1 {
			copies = 1
		}
		*pCopies = int16(copies)

		*fields |= DM_ORIENTATION
		if strings.EqualFold(orientation, "landscape") {
			*pOrient = DMORIENT_LANDSCAPE
		} else {
			*pOrient = DMORIENT_PORTRAIT
		}

		*fields |= DM_PAPERSIZE
		ps := strings.ToLower(paperSize)
		if strings.Contains(ps, "a5") {
			*pPaper = DMPAPER_A5
		} else if strings.Contains(ps, "letter") {
			*pPaper = DMPAPER_LETTER
		} else {
			*pPaper = DMPAPER_A4
		}

		*fields |= DM_COLOR
		if strings.EqualFold(colorMode, "monochrome") {
			*pColor = DMCOLOR_MONOCHROME
		} else {
			*pColor = DMCOLOR_COLOR
		}

		// Merge modifications back through DocumentPropertiesW
		procDocumentPropertiesW.Call(
			0, hPrinter,
			uintptr(unsafe.Pointer(pDevice)),
			uintptr(unsafe.Pointer(&devModeBuf[0])),
			uintptr(unsafe.Pointer(&devModeBuf[0])),
			uintptr(DM_IN_BUFFER|DM_OUT_BUFFER),
		)
	}

	return devModeBuf
}

// PrintDocumentWin32 prints formatted documents directly to standard desktop printers
// (HP Smart Tank, LaserJet, Inkjet) using 100% native Windows GDI.
// ZERO child processes are launched. NO terminal, cmd, or powershell window can EVER appear!
func PrintDocumentWin32(printerName, docName, content, paperSize, orientation, colorMode string, copies int) error {
	if printerName == "" {
		def, err := GetDefaultPrinterName()
		if err != nil || def == "" {
			return fmt.Errorf("printer name is required and no default printer found")
		}
		printerName = def
	}
	if copies < 1 {
		copies = 1
	}

	pDevice, err := syscall.UTF16PtrFromString(printerName)
	if err != nil {
		return fmt.Errorf("invalid printer name: %w", err)
	}

	var hPrinter uintptr
	rOpen, _, _ := procOpenPrinterW.Call(uintptr(unsafe.Pointer(pDevice)), uintptr(unsafe.Pointer(&hPrinter)), 0)
	var devModeBuf []byte
	if rOpen != 0 && hPrinter != 0 {
		defer procClosePrinter.Call(hPrinter)
		devModeBuf = createDevMode(hPrinter, printerName, paperSize, orientation, colorMode, copies)
	}

	pDriver, _ := syscall.UTF16PtrFromString("WINSPOOL")
	var pInit uintptr
	if len(devModeBuf) > 0 {
		pInit = uintptr(unsafe.Pointer(&devModeBuf[0]))
	}

	hdc, _, errDC := procCreateDCW.Call(
		uintptr(unsafe.Pointer(pDriver)),
		uintptr(unsafe.Pointer(pDevice)),
		0,
		pInit,
	)
	if hdc == 0 {
		return fmt.Errorf("failed to create printer device context for %q: %v", printerName, errDC)
	}
	defer procDeleteDC.Call(hdc)

	dpiX, _, _ := procGetDeviceCaps.Call(hdc, uintptr(LOGPIXELSX))
	if dpiX == 0 {
		dpiX = 300
	}
	dpiY, _, _ := procGetDeviceCaps.Call(hdc, uintptr(LOGPIXELSY))
	if dpiY == 0 {
		dpiY = 300
	}
	pageHeight, _, _ := procGetDeviceCaps.Call(hdc, uintptr(VERTRES))

	pFontName, _ := syscall.UTF16PtrFromString("Consolas")
	// Font size: 10pt = (10 * dpiY) / 72
	fontHeight := int32((10 * dpiY) / 72)
	lineSpacing := int32((13 * dpiY) / 72)

	hFontReg, _, _ := procCreateFontW.Call(
		uintptr(fontHeight), 0, 0, 0, uintptr(FW_NORMAL),
		0, 0, 0, uintptr(ANSI_CHARSET),
		uintptr(OUT_DEFAULT_PRECIS), uintptr(CLIP_DEFAULT_PRECIS),
		uintptr(DEFAULT_QUALITY), uintptr(DEFAULT_PITCH),
		uintptr(unsafe.Pointer(pFontName)),
	)
	defer procDeleteObject.Call(hFontReg)

	hFontBold, _, _ := procCreateFontW.Call(
		uintptr(fontHeight), 0, 0, 0, uintptr(FW_BOLD),
		0, 0, 0, uintptr(ANSI_CHARSET),
		uintptr(OUT_DEFAULT_PRECIS), uintptr(CLIP_DEFAULT_PRECIS),
		uintptr(DEFAULT_QUALITY), uintptr(DEFAULT_PITCH),
		uintptr(unsafe.Pointer(pFontName)),
	)
	defer procDeleteObject.Call(hFontBold)

	pDocName, _ := syscall.UTF16PtrFromString(docName)
	docInfo := DOCINFOW{
		cbSize:      int32(unsafe.Sizeof(DOCINFOW{})),
		lpszDocName: pDocName,
	}

	rDoc, _, errDoc := procStartDocW.Call(hdc, uintptr(unsafe.Pointer(&docInfo)))
	if int32(rDoc) <= 0 {
		return fmt.Errorf("StartDoc failed on %s: %v", printerName, errDoc)
	}
	defer procEndDoc.Call(hdc)

	procStartPage.Call(hdc)

	// Half-inch margins
	leftMargin := int32(dpiX / 2)
	topMargin := int32(dpiY / 2)
	bottomMargin := int32(pageHeight) - topMargin

	currentY := topMargin
	procSelectObject.Call(hdc, hFontReg)

	lines := strings.Split(content, "\n")
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if currentY+lineSpacing > bottomMargin {
			procEndPage.Call(hdc)
			procStartPage.Call(hdc)
			currentY = topMargin
		}

		isBold := strings.Contains(line, "==") ||
			strings.Contains(line, "SIRAAZHI") ||
			strings.Contains(line, "TOTAL") ||
			strings.Contains(line, "TAX INVOICE") ||
			strings.Contains(line, "CASH RECEIPT") ||
			strings.Contains(line, "CASH BILL")

		if isBold {
			procSelectObject.Call(hdc, hFontBold)
		} else {
			procSelectObject.Call(hdc, hFontReg)
		}

		u16Line, _ := syscall.UTF16FromString(line)
		if len(u16Line) > 1 {
			procTextOutW.Call(
				hdc,
				uintptr(leftMargin),
				uintptr(currentY),
				uintptr(unsafe.Pointer(&u16Line[0])),
				uintptr(len(u16Line)-1),
			)
		}
		currentY += lineSpacing
	}

	procEndPage.Call(hdc)
	return nil
}

// IsThermalPOSPrinter returns true if the printer name indicates a standard 80mm/58mm thermal receipt printer
func IsThermalPOSPrinter(name string) bool {
	n := strings.ToLower(name)
	// Non-thermal desktop office printers (HP Smart Tank, Deskjet, LaserJet, Epson EcoTank, Canon Pixma, Brother)
	if strings.Contains(n, "smart tank") ||
		strings.Contains(n, "deskjet") ||
		strings.Contains(n, "laserjet") ||
		strings.Contains(n, "laser") ||
		strings.Contains(n, "ink tank") ||
		strings.Contains(n, "ecotank") ||
		strings.Contains(n, "l3") || strings.Contains(n, "l5") ||
		strings.Contains(n, "pixma") ||
		strings.Contains(n, "brother") ||
		strings.Contains(n, "pdf") ||
		strings.Contains(n, "xps") ||
		strings.Contains(n, "onenote") {
		return false
	}
	return true
}
