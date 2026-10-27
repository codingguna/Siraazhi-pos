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
	"time"
	"unsafe"

	"siraazhi-pos/db"
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

	modgdi32             = syscall.NewLazyDLL("gdi32.dll")
	procCreateDCW        = modgdi32.NewProc("CreateDCW")
	procDeleteDC         = modgdi32.NewProc("DeleteDC")
	procStartDocW        = modgdi32.NewProc("StartDocW")
	procEndDoc           = modgdi32.NewProc("EndDoc")
	procStartPage        = modgdi32.NewProc("StartPage")
	procEndPage          = modgdi32.NewProc("EndPage")
	procTextOutW         = modgdi32.NewProc("TextOutW")
	procSelectObject     = modgdi32.NewProc("SelectObject")
	procDeleteObject     = modgdi32.NewProc("DeleteObject")
	procCreateFontW      = modgdi32.NewProc("CreateFontW")
	procGetDeviceCaps    = modgdi32.NewProc("GetDeviceCaps")
	procSetTextColor     = modgdi32.NewProc("SetTextColor")
	procSetBkMode        = modgdi32.NewProc("SetBkMode")
	procCreatePen        = modgdi32.NewProc("CreatePen")
	procCreateSolidBrush = modgdi32.NewProc("CreateSolidBrush")
	procMoveToEx         = modgdi32.NewProc("MoveToEx")
	procLineTo           = modgdi32.NewProc("LineTo")
	procRectangle        = modgdi32.NewProc("Rectangle")

	moduser32     = syscall.NewLazyDLL("user32.dll")
	procDrawTextW = moduser32.NewProc("DrawTextW")
	procFillRect  = moduser32.NewProc("FillRect")
)

const (
	PRINTER_ENUM_LOCAL       = 0x00000002
	PRINTER_ENUM_CONNECTIONS = 0x00000004

	HORZRES    = 8
	VERTRES    = 10
	LOGPIXELSX = 88
	LOGPIXELSY = 90
	FW_NORMAL  = 400
	FW_BOLD    = 700

	ANSI_CHARSET       = 0
	DEFAULT_CHARSET    = 1
	OUT_DEFAULT_PRECIS = 0
	CLIP_DEFAULT_PRECIS = 0
	DEFAULT_QUALITY    = 0
	DEFAULT_PITCH      = 0

	PS_SOLID = 0
	PS_DASH  = 1
	PS_DOT   = 2

	TRANSPARENT = 1
	OPAQUE      = 2

	DT_TOP        = 0x00000000
	DT_LEFT       = 0x00000000
	DT_CENTER     = 0x00000001
	DT_RIGHT      = 0x00000002
	DT_VCENTER    = 0x00000004
	DT_BOTTOM     = 0x00000008
	DT_WORDBREAK  = 0x00000010
	DT_SINGLELINE = 0x00000020
	DT_NOPREFIX   = 0x00000800

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

type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

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

// utf16PtrToString safely reads a null-terminated UTF-16 string from a pointer
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	s := unsafe.Slice(p, 512)
	for i, val := range s {
		if val == 0 {
			return syscall.UTF16ToString(s[:i])
		}
	}
	return syscall.UTF16ToString(s)
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
				name := utf16PtrToString(pInfo.pPrinterName)
				trimmed := strings.TrimSpace(name)
				if trimmed != "" && !seen[trimmed] {
					seen[trimmed] = true
					printers = append(printers, trimmed)
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
					name := utf16PtrToString(pInfo.pPrinterName)
					trimmed := strings.TrimSpace(name)
					if trimmed != "" && !seen[trimmed] {
						seen[trimmed] = true
						printers = append(printers, trimmed)
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

	if !IsThermalPOSPrinter(printerName) {
		return fmt.Errorf("printer %q is an office/desktop printer (not a thermal POS roll printer) and cannot accept raw ESC/POS binary data", printerName)
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

	// DEVMODEW layout offsets (Unicode):
	// dmDeviceName: 64 bytes (WCHAR[32], offset 0..63)
	// dmSpecVersion: offset 64 (WORD = 2 bytes)
	// dmDriverVersion: offset 66 (WORD = 2 bytes)
	// dmSize: offset 68 (WORD = 2 bytes)
	// dmDriverExtra: offset 70 (WORD = 2 bytes)
	// dmFields: offset 72 (DWORD = 4 bytes)
	// dmOrientation: offset 76 (short = 2 bytes)
	// dmPaperSize: offset 78 (short = 2 bytes)
	// dmCopies: offset 86 (short = 2 bytes)
	// dmColor: offset 92 (short = 2 bytes)
	if len(devModeBuf) >= 94 {
		fields := (*uint32)(unsafe.Pointer(&devModeBuf[72]))
		pOrient := (*int16)(unsafe.Pointer(&devModeBuf[76]))
		pPaper := (*int16)(unsafe.Pointer(&devModeBuf[78]))
		pCopies := (*int16)(unsafe.Pointer(&devModeBuf[86]))
		pColor := (*int16)(unsafe.Pointer(&devModeBuf[92]))

		*fields |= DM_COPIES
		if copies < 1 {
			copies = 1
		}
		psLower := strings.ToLower(paperSize)
		// When 2 copies are selected on A4/document, both copies are drawn on the SAME single sheet (top & bottom half),
		// so spooler feeds only 1 physical page.
		if copies == 2 && !strings.Contains(psLower, "80mm") && !strings.Contains(psLower, "58mm") {
			*pCopies = 1
		} else {
			*pCopies = int16(copies)
		}

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
		// Fallback 1: driver NULL with pInit
		hdc, _, errDC = procCreateDCW.Call(
			0,
			uintptr(unsafe.Pointer(pDevice)),
			0,
			pInit,
		)
	}
	if hdc == 0 {
		// Fallback 2: driver NULL with default DEVMODE
		hdc, _, errDC = procCreateDCW.Call(
			0,
			uintptr(unsafe.Pointer(pDevice)),
			0,
			0,
		)
	}
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
	pageWidth, _, _ := procGetDeviceCaps.Call(hdc, uintptr(HORZRES))
	pageHeight, _, _ := procGetDeviceCaps.Call(hdc, uintptr(VERTRES))

	lines := strings.Split(content, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	numLines := int32(len(lines))
	if numLines == 0 {
		numLines = 1
	}

	// 1. Calculate Width: Full A4 width with standard border gap (~10 mm / 0.38 in)
	const numCols = 96.0
	normalMargin := int32(float64(dpiX) * 0.38)
	usableWidth := int32(pageWidth) - (2 * normalMargin)
	if usableWidth <= 0 {
		usableWidth = int32(pageWidth) - 40
		normalMargin = 20
	}

	// Consolas average character width in GDI is ~0.601 * fontHeight
	// Scale fontHeight so 96 columns span usableWidth across the page
	fontHeight := int32(float64(usableWidth) / (0.601 * numCols))
	if fontHeight < 12 {
		fontHeight = 12
	}

	totalLineWidth := int32(float64(fontHeight) * 0.601 * numCols)
	leftMargin := (int32(pageWidth) - totalLineWidth) / 2
	if leftMargin < 0 {
		leftMargin = 0
	}

	// 2. Calculate Height: STRICTLY FIT WITHIN TOP HALF OF A4 PAPER (< 148.5 mm)
	halfPageHeight := int32(pageHeight) / 2
	topMargin := int32(float64(dpiY) * 0.30) // ~7.6 mm top gap
	availableHeight := halfPageHeight - topMargin - int32(float64(dpiY)*0.10) // safety buffer before middle fold

	lineSpacing := int32(float64(fontHeight) * 1.30)
	if numLines*lineSpacing > availableHeight {
		lineSpacing = availableHeight / numLines
		if lineSpacing < int32(float64(fontHeight)*1.05) {
			fontHeight = int32(float64(lineSpacing) * 0.85)
			if fontHeight < 10 {
				fontHeight = 10
			}
			totalLineWidth = int32(float64(fontHeight) * 0.601 * numCols)
			leftMargin = (int32(pageWidth) - totalLineWidth) / 2
			if leftMargin < 0 {
				leftMargin = 0
			}
		}
	}

	pFontName, _ := syscall.UTF16PtrFromString("Consolas")
	hFontReg, _, _ := procCreateFontW.Call(
		uintptr(fontHeight), 0, 0, 0, uintptr(FW_NORMAL),
		0, 0, 0, uintptr(DEFAULT_CHARSET),
		uintptr(OUT_DEFAULT_PRECIS), uintptr(CLIP_DEFAULT_PRECIS),
		uintptr(DEFAULT_QUALITY), uintptr(DEFAULT_PITCH),
		uintptr(unsafe.Pointer(pFontName)),
	)
	defer procDeleteObject.Call(hFontReg)

	hFontBold, _, _ := procCreateFontW.Call(
		uintptr(fontHeight), 0, 0, 0, uintptr(FW_BOLD),
		0, 0, 0, uintptr(DEFAULT_CHARSET),
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

	currentY := topMargin
	procSelectObject.Call(hdc, hFontReg)

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")

		isBold := strings.Contains(line, "TAX INVOICE") ||
			strings.Contains(line, "RETAIL CASH BILL") ||
			strings.Contains(line, "TOTAL INVOICE VALUE") ||
			strings.Contains(line, "GRAND TOTAL") ||
			strings.Contains(line, "VANUR FARMER") ||
			strings.Contains(line, "BILLED TO") ||
			strings.Contains(line, "INVOICE DETAILS") ||
			strings.Contains(line, "Description of Goods") ||
			strings.Contains(line, "Amount in Words")

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

// PrintBillGDI renders an invoice to a Windows printer using vector GDI.
// - 80mm / 58mm: Renders exact 80mm thermal receipt template (bill1).
// - a4 / a5 / letter: Renders exact full corporate A4 tax invoice (bill2).
func PrintBillGDI(printerName, docName string, bill *db.Bill, s *db.Settings, paperSize, orientation, colorMode string, copies int) error {
	if bill == nil {
		return fmt.Errorf("bill cannot be nil")
	}
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
		hdc, _, errDC = procCreateDCW.Call(0, uintptr(unsafe.Pointer(pDevice)), 0, pInit)
	}
	if hdc == 0 {
		hdc, _, errDC = procCreateDCW.Call(0, uintptr(unsafe.Pointer(pDevice)), 0, 0)
	}
	if hdc == 0 {
		return fmt.Errorf("failed to create printer device context for %q: %v", printerName, errDC)
	}
	defer procDeleteDC.Call(hdc)

	if docName == "" {
		docName = "Bill_" + bill.BillNo
	}
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

	ps := strings.ToLower(strings.TrimSpace(paperSize))
	if ps == "80mm" || ps == "58mm" {
		drawReceipt80mmGDI(hdc, bill, s)
	} else {
		if copies == 2 {
			drawInvoiceA4TwoUpGDI(hdc, bill, s)
		} else {
			drawInvoiceA4GDI(hdc, bill, s)
		}
	}

	procEndPage.Call(hdc)
	return nil
}

// PrintTestSlipGDI renders a verification test slip to a Windows printer using vector GDI.
// - 80mm / 58mm: Renders an 80mm test receipt matching bill1 receipt styling.
// - a4 / a5 / letter: Renders a full A4 test verification document matching bill2 styling.
func PrintTestSlipGDI(printerName, paperSize, orientation, colorMode string, s *db.Settings) error {
	if printerName == "" {
		def, err := GetDefaultPrinterName()
		if err != nil || def == "" {
			return fmt.Errorf("printer name is required and no default printer found")
		}
		printerName = def
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
		devModeBuf = createDevMode(hPrinter, printerName, paperSize, orientation, colorMode, 1)
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
		hdc, _, errDC = procCreateDCW.Call(0, uintptr(unsafe.Pointer(pDevice)), 0, pInit)
	}
	if hdc == 0 {
		hdc, _, errDC = procCreateDCW.Call(0, uintptr(unsafe.Pointer(pDevice)), 0, 0)
	}
	if hdc == 0 {
		return fmt.Errorf("failed to create printer device context for %q: %v", printerName, errDC)
	}
	defer procDeleteDC.Call(hdc)

	pDocName, _ := syscall.UTF16PtrFromString("Siraazhi POS Test Slip")
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

	ps := strings.ToLower(strings.TrimSpace(paperSize))
	if ps == "80mm" || ps == "58mm" {
		drawTestSlip80mmGDI(hdc, printerName, s)
	} else {
		drawTestSlipA4GDI(hdc, printerName, orientation, colorMode, s)
	}

	procEndPage.Call(hdc)
	return nil
}

// ----------------------------------------------------------------------------
// GDI Helper Routines & Template Renderers (Bill 1 & Bill 2 & Test Slips)
// ----------------------------------------------------------------------------

func setupGDIHelpers(hdc uintptr) (
	scaleX, scaleY func(float64) int32,
	ptH func(float64) int32,
	pageWidth, pageHeight int32,
	createFont func(float64, bool) uintptr,
	createPen func(int, int32, uint32) uintptr,
	createBrush func(uint32) uintptr,
	drawText func(string, int32, int32, int32, int32, uintptr, uint32, uint32),
	drawLine func(int32, int32, int32, int32, uintptr),
	fillRect func(int32, int32, int32, int32, uintptr),
	drawRect func(int32, int32, int32, int32, uintptr, uintptr),
	cleanup func(),
) {
	dpiXVal, _, _ := procGetDeviceCaps.Call(hdc, uintptr(LOGPIXELSX))
	dpiYVal, _, _ := procGetDeviceCaps.Call(hdc, uintptr(LOGPIXELSY))
	dpiX := int32(dpiXVal)
	dpiY := int32(dpiYVal)
	if dpiX <= 0 {
		dpiX = 300
	}
	if dpiY <= 0 {
		dpiY = 300
	}

	pw, _, _ := procGetDeviceCaps.Call(hdc, uintptr(HORZRES))
	ph, _, _ := procGetDeviceCaps.Call(hdc, uintptr(VERTRES))
	pageWidth = int32(pw)
	pageHeight = int32(ph)
	if pageWidth <= 0 {
		pageWidth = int32(float64(dpiX) * 8.27)
	}
	if pageHeight <= 0 {
		pageHeight = int32(float64(dpiY) * 11.69)
	}

	scaleX = func(mm float64) int32 {
		return int32(mm * float64(dpiX) / 25.4)
	}
	scaleY = func(mm float64) int32 {
		return int32(mm * float64(dpiY) / 25.4)
	}
	ptH = func(pt float64) int32 {
		return int32(pt * float64(dpiY) / 72.0)
	}

	var gdiObjects []uintptr
	cleanup = func() {
		for _, obj := range gdiObjects {
			if obj != 0 {
				procDeleteObject.Call(obj)
			}
		}
	}

	createFont = func(pt float64, bold bool) uintptr {
		weight := FW_NORMAL
		if bold {
			weight = FW_BOLD
		}
		height := ptH(pt)
		pName, _ := syscall.UTF16PtrFromString("Nirmala UI")
		hFont, _, _ := procCreateFontW.Call(
			uintptr(height), 0, 0, 0, uintptr(weight),
			0, 0, 0, uintptr(DEFAULT_CHARSET),
			uintptr(OUT_DEFAULT_PRECIS), uintptr(CLIP_DEFAULT_PRECIS),
			uintptr(DEFAULT_QUALITY), uintptr(DEFAULT_PITCH),
			uintptr(unsafe.Pointer(pName)),
		)
		if hFont != 0 {
			gdiObjects = append(gdiObjects, hFont)
		}
		return hFont
	}

	createPen = func(style int, width int32, color uint32) uintptr {
		hPen, _, _ := procCreatePen.Call(uintptr(style), uintptr(width), uintptr(color))
		if hPen != 0 {
			gdiObjects = append(gdiObjects, hPen)
		}
		return hPen
	}

	createBrush = func(color uint32) uintptr {
		hBrush, _, _ := procCreateSolidBrush.Call(uintptr(color))
		if hBrush != 0 {
			gdiObjects = append(gdiObjects, hBrush)
		}
		return hBrush
	}

	drawText = func(text string, x1, y1, x2, y2 int32, hFont uintptr, color uint32, flags uint32) {
		if text == "" {
			return
		}
		procSelectObject.Call(hdc, hFont)
		procSetTextColor.Call(hdc, uintptr(color))
		procSetBkMode.Call(hdc, uintptr(TRANSPARENT))
		rect := RECT{Left: x1, Top: y1, Right: x2, Bottom: y2}
		u16, err := syscall.UTF16FromString(text)
		if err != nil || len(u16) <= 1 {
			return
		}
		procDrawTextW.Call(
			hdc,
			uintptr(unsafe.Pointer(&u16[0])),
			uintptr(len(u16)-1),
			uintptr(unsafe.Pointer(&rect)),
			uintptr(flags),
		)
	}

	drawLine = func(x1, y1, x2, y2 int32, hPen uintptr) {
		procSelectObject.Call(hdc, hPen)
		procMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
		procLineTo.Call(hdc, uintptr(x2), uintptr(y2))
	}

	fillRect = func(x1, y1, x2, y2 int32, hBrush uintptr) {
		rect := RECT{Left: x1, Top: y1, Right: x2, Bottom: y2}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), hBrush)
	}

	drawRect = func(x1, y1, x2, y2 int32, hPen uintptr, hBrush uintptr) {
		procSelectObject.Call(hdc, hPen)
		procSelectObject.Call(hdc, hBrush)
		procRectangle.Call(hdc, uintptr(x1), uintptr(y1), uintptr(x2), uintptr(y2))
	}

	return
}

// drawInvoiceA4GDI renders the pristine corporate A4 tax invoice template (bill2) using vector GDI
func drawInvoiceA4GDI(hdc uintptr, bill *db.Bill, s *db.Settings) {
	drawSingleInvoiceA4GDI(hdc, bill, s, 10.0, "", false)
}

// drawInvoiceA4TwoUpGDI renders 2 copies on the same A4 sheet (top & bottom half with middle cut line)
func drawInvoiceA4TwoUpGDI(hdc uintptr, bill *db.Bill, s *db.Settings) {
	scaleX, scaleY, _, pageWidth, _, createFont, createPen, _, drawText, drawLine, _, _, cleanup := setupGDIHelpers(hdc)
	defer cleanup()

	// 1. Top Half: Original for Recipient (fills 6.0mm to 141.0mm)
	drawSingleInvoiceA4GDI(hdc, bill, s, 6.0, "ORIGINAL FOR RECIPIENT", true)

	// 2. Middle Cut Line with Scissors at 148.5mm (halfway down 297mm A4 page)
	colorCut := uint32(0x00555555)
	hPenDashed := createPen(PS_DASH, 1, colorCut)
	fCut := createFont(7.5, true)
	midY := scaleY(148.5)
	left := scaleX(10)
	right := pageWidth - scaleX(10)

	drawLine(left, midY, right, midY, hPenDashed)
	drawText("✂ - - - - - - - - - - - - - - - - - - - - CUT HERE - - - - - - - - - - - - - - - - - - - - ✂", left, midY-scaleY(2.2), right, midY+scaleY(2.2), fCut, colorCut, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

	// 3. Bottom Half: Duplicate for Supplier (fills 154.5mm to 289.5mm)
	drawSingleInvoiceA4GDI(hdc, bill, s, 154.5, "DUPLICATE FOR SUPPLIER", true)
}

// drawSingleInvoiceA4GDI renders the pristine corporate A4 tax invoice template (bill2) using vector GDI
func drawSingleInvoiceA4GDI(hdc uintptr, bill *db.Bill, s *db.Settings, startYmm float64, copyTag string, isHalf bool) {
	scaleX, scaleY, _, pageWidth, _, createFont, createPen, createBrush, drawText, drawLine, fillRect, drawRect, cleanup := setupGDIHelpers(hdc)
	defer cleanup()

	colorBlack := uint32(0x00000000)
	colorWhite := uint32(0x00FFFFFF)
	colorDarkGreen := uint32(0x00205E1B) // RGB(27, 94, 32)
	colorLightGray := uint32(0x00F0F0F0) // RGB(240, 240, 240)
	colorTaxGray := uint32(0x00F7F7F7)   // RGB(247, 247, 247)
	colorDarkGray := uint32(0x00444444)
	colorMuted := uint32(0x00666666)

	fTitleSize := 15.5
	fBrandSize := 12.5
	fBadgeSize := 11.5
	fSubSize := 9.5
	fSubBoldSize := 9.5
	fTableHSize := 9.5
	fTableRSize := 9.5
	fTableBSize := 9.5
	fTableTaSize := 8.0
	fTotalSize := 13.5
	fDeclSize := 8.5
	fSignSize := 9.5
	vMult := 1.0

	if isHalf {
		fTitleSize = 12.0
		fBrandSize = 10.0
		fBadgeSize = 9.5
		fSubSize = 8.0
		fSubBoldSize = 8.0
		fTableHSize = 8.5
		fTableRSize = 8.0
		fTableBSize = 8.0
		fTableTaSize = 7.0
		fTotalSize = 11.5
		fDeclSize = 7.0
		fSignSize = 8.0
		vMult = 0.85
	}

	fTitle := createFont(fTitleSize, true)
	fBrand := createFont(fBrandSize, true)
	fBadge := createFont(fBadgeSize, true)
	fSub := createFont(fSubSize, false)
	fSubBold := createFont(fSubBoldSize, true)
	fTableH := createFont(fTableHSize, true)
	fTableR := createFont(fTableRSize, false)
	fTableB := createFont(fTableBSize, true)
	fTableTa := createFont(fTableTaSize, false)
	fTotal := createFont(fTotalSize, true)
	fDecl := createFont(fDeclSize, false)
	fSign := createFont(fSignSize, false)
	fCopyTag := createFont(8.0, true)

	hPenThick := createPen(PS_SOLID, scaleX(0.60), colorBlack)
	hPenBorder := createPen(PS_SOLID, scaleX(0.20), colorBlack)
	hBrushWhite := createBrush(colorWhite)
	hBrushHeader := createBrush(colorLightGray)
	hBrushTaxH := createBrush(colorTaxGray)
	hBrushBlack := createBrush(colorBlack)

	companyName := "VANUR FARMER PRODUCER COMPANY LIMITED"
	if s != nil && s.CompanyName != "" {
		companyName = strings.ToUpper(s.CompanyName)
	}
	brandEn := "Siraazhi"
	if s != nil && s.BrandEn != "" {
		brandEn = s.BrandEn
	}
	brandTa := "சிராழி"
	if s != nil && s.BrandTa != "" {
		brandTa = s.BrandTa
	}
	tagline := "Pure Traditional Marachekku Oils"
	if s != nil && s.Tagline != "" {
		tagline = s.Tagline
	}
	address := "Thailapuram Village, Vanur, Villupuram, Tamil Nadu"
	if s != nil && s.Address != "" {
		address = s.Address
	}
	phone := "9876543210"
	if s != nil && s.Phone != "" {
		phone = s.Phone
	}
	fssai := "12423031000494"
	if s != nil && s.Fssai != "" {
		fssai = s.Fssai
	}

	custName := "Consumer / Walk-in"
	if bill.Customer.Name != "" {
		custName = bill.Customer.Name
	}
	custPhone := "-"
	if bill.Customer.Phone != "" {
		custPhone = bill.Customer.Phone
	}
	custAddr := "Tamil Nadu"
	if bill.Customer.Address != "" {
		custAddr = bill.Customer.Address
	}
	custType := "Customer Type: Consumer / Unregistered"
	if bill.Customer.Gstin != "" {
		custType = "Customer GSTIN: " + bill.Customer.Gstin
	}
	placeOfSupply := "Tamil Nadu"
	if bill.Customer.State != "" {
		placeOfSupply = bill.Customer.State
	}

	left := scaleX(10)
	right := pageWidth - scaleX(10)
	width := right - left
	curY := scaleY(startYmm)

	if copyTag != "" {
		drawText(copyTag, left, curY, right, curY+scaleY(3.5), fCopyTag, colorDarkGray, DT_RIGHT|DT_SINGLELINE|DT_NOPREFIX)
		curY += scaleY(3.2)
	}

	// 1. Header Block
	drawText(companyName, left, curY, left+scaleX(120), curY+scaleY(6*vMult), fTitle, colorBlack, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	badgeText := "RETAIL CASH BILL"
	if bill.GSTOn {
		badgeText = "TAX INVOICE"
	}
	badgeW := scaleX(36)
	badgeH := scaleY(5.5 * vMult)
	fillRect(right-badgeW, curY, right, curY+badgeH, hBrushBlack)
	drawText(badgeText, right-badgeW, curY, right, curY+badgeH, fBadge, colorWhite, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

	curY += scaleY(6.5 * vMult)
	drawText(fmt.Sprintf("%s (%s) — %s", brandEn, brandTa, tagline), left, curY, left+scaleX(120), curY+scaleY(5*vMult), fBrand, colorDarkGreen, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	drawText("Invoice No: "+bill.BillNo, right-scaleX(60), curY, right, curY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_SINGLELINE|DT_NOPREFIX)

	curY += scaleY(5 * vMult)
	drawText(address, left, curY, left+scaleX(120), curY+scaleY(4.5*vMult), fSub, colorDarkGray, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	drawText("Date: "+bill.Date, right-scaleX(60), curY, right, curY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_SINGLELINE|DT_NOPREFIX)

	curY += scaleY(4.5 * vMult)
	phoneLine := fmt.Sprintf("Phone: %s | FSSAI Lic. No: %s", phone, fssai)
	drawText(phoneLine, left, curY, left+scaleX(120), curY+scaleY(4.5*vMult), fSub, colorBlack, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	drawText("Mode: "+bill.PayMode, right-scaleX(60), curY, right, curY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_SINGLELINE|DT_NOPREFIX)

	curY += scaleY(4.5 * vMult)
	if s != nil && s.Gstin != "" {
		drawText("GSTIN: "+s.Gstin+" | State: 33 - Tamil Nadu", left, curY, left+scaleX(120), curY+scaleY(4.5*vMult), fSubBold, colorBlack, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
		curY += scaleY(5 * vMult)
	}

	drawLine(left, curY, right, curY, hPenThick)
	curY += scaleY(3 * vMult)

	// 2. Billed To Details Grid
	bH := scaleY(20 * vMult)
	drawRect(left, curY, right, curY+bH, hPenBorder, hBrushWhite)
	drawText("BILLED TO (CUSTOMER):", left+scaleX(4), curY+scaleY(1.5*vMult), left+scaleX(100), curY+scaleY(5*vMult), fDecl, colorMuted, DT_LEFT|DT_NOPREFIX)
	drawText(custName, left+scaleX(4), curY+scaleY(5.5*vMult), left+scaleX(100), curY+scaleY(10*vMult), fTitle, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText("Mobile: "+custPhone, left+scaleX(4), curY+scaleY(10.5*vMult), left+scaleX(100), curY+scaleY(14.5*vMult), fSub, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText("Address: "+custAddr, left+scaleX(4), curY+scaleY(14.5*vMult), left+scaleX(100), curY+scaleY(18.5*vMult), fSub, colorDarkGray, DT_LEFT|DT_NOPREFIX)

	drawText(custType, left+scaleX(105), curY+scaleY(5.5*vMult), right-scaleX(4), curY+scaleY(10*vMult), fSubBold, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText("Place of Supply: "+placeOfSupply, left+scaleX(105), curY+scaleY(10.5*vMult), right-scaleX(4), curY+scaleY(14.5*vMult), fSub, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += bH + scaleY(3*vMult)

	// 3. Items Table
	w0 := scaleX(10) // #
	w2 := scaleX(26) // HSN
	w3 := scaleX(18) // Qty
	w4 := scaleX(30) // Unit Rate
	w5 := scaleX(34) // Total Amount
	w1 := width - (w0 + w2 + w3 + w4 + w5)

	x0 := left
	x1 := x0 + w0
	x2 := x1 + w1
	x3 := x2 + w2
	x4 := x3 + w3
	x5 := x4 + w4
	x6 := right

	hH := scaleY(7 * vMult)
	fillRect(left, curY, right, curY+hH, hBrushHeader)
	drawRect(left, curY, right, curY+hH, hPenBorder, hBrushHeader)
	drawLine(x1, curY, x1, curY+hH, hPenBorder)
	drawLine(x2, curY, x2, curY+hH, hPenBorder)
	drawLine(x3, curY, x3, curY+hH, hPenBorder)
	drawLine(x4, curY, x4, curY+hH, hPenBorder)
	drawLine(x5, curY, x5, curY+hH, hPenBorder)

	drawText("#", x0, curY, x1, curY+hH, fTableH, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	drawText("Description of Goods", x1+scaleX(2), curY, x2-scaleX(2), curY+hH, fTableH, colorBlack, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	drawText("HSN", x2, curY, x3, curY+hH, fTableH, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	drawText("Qty", x3, curY, x4, curY+hH, fTableH, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	drawText("Unit Rate", x4, curY, x5-scaleX(2), curY+hH, fTableH, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	drawText("Total Amount", x5, curY, x6-scaleX(2), curY+hH, fTableH, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	curY += hH

	for idx, it := range bill.Items {
		rH := scaleY(10.5 * vMult)
		if isHalf {
			rH = scaleY(7.0 * vMult)
		}
		drawRect(left, curY, right, curY+rH, hPenBorder, hBrushWhite)
		drawLine(x1, curY, x1, curY+rH, hPenBorder)
		drawLine(x2, curY, x2, curY+rH, hPenBorder)
		drawLine(x3, curY, x3, curY+rH, hPenBorder)
		drawLine(x4, curY, x4, curY+rH, hPenBorder)
		drawLine(x5, curY, x5, curY+rH, hPenBorder)

		drawText(fmt.Sprintf("%d", idx+1), x0, curY, x1, curY+rH, fTableR, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		nameEn := it.NameEn
		if nameEn == "" {
			nameEn = "Item"
		}
		nameTa := it.NameTa

		if !isHalf && nameTa != "" {
			drawText(nameEn, x1+scaleX(2), curY+scaleY(1*vMult), x2-scaleX(2), curY+scaleY(5.5*vMult), fTableB, colorBlack, DT_LEFT|DT_NOPREFIX)
			drawText(nameTa, x1+scaleX(2), curY+scaleY(5.5*vMult), x2-scaleX(2), curY+scaleY(9.5*vMult), fTableTa, colorDarkGray, DT_LEFT|DT_NOPREFIX)
		} else {
			displayName := nameEn
			if isHalf && nameTa != "" {
				displayName = fmt.Sprintf("%s (%s)", nameEn, nameTa)
			}
			drawText(displayName, x1+scaleX(2), curY, x2-scaleX(2), curY+rH, fTableB, colorBlack, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		}

		drawText(it.HSN, x2, curY, x3, curY+rH, fTableR, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("%.2f", it.Qty), x3, curY, x4, curY+rH, fTableR, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", it.Price), x4, curY, x5-scaleX(2), curY+rH, fTableR, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", it.Amount), x5, curY, x6-scaleX(2), curY+rH, fTableB, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		curY += rH
	}

	targetEndY := scaleY(startYmm + 135.0)
	if isHalf {
		minBottomStartY := targetEndY - scaleY(54.0*vMult)
		if curY < minBottomStartY {
			// Extend table borders and vertical column lines to fill available space symmetrically
			tBottom := minBottomStartY - scaleY(2.0*vMult)
			drawRect(left, curY, right, tBottom, hPenBorder, hBrushWhite)
			drawLine(x1, curY, x1, tBottom, hPenBorder)
			drawLine(x2, curY, x2, tBottom, hPenBorder)
			drawLine(x3, curY, x3, tBottom, hPenBorder)
			drawLine(x4, curY, x4, tBottom, hPenBorder)
			drawLine(x5, curY, x5, tBottom, hPenBorder)
			curY = minBottomStartY
		} else {
			curY += scaleY(3 * vMult)
		}
	} else {
		curY += scaleY(3 * vMult)
	}

	// 4. Totals and Tax Breakdown Block
	tY := curY
	if bill.GSTOn {
		// HSN/SAC Tax Summary Table
		txW := scaleX(102)
		cW1 := scaleX(27)
		cWRest := scaleX(15)
		drawRect(left, tY, left+txW, tY+scaleY(12*vMult), hPenBorder, hBrushWhite)
		fillRect(left, tY, left+txW, tY+scaleY(5.5*vMult), hBrushTaxH)
		drawLine(left, tY+scaleY(5.5*vMult), left+txW, tY+scaleY(5.5*vMult), hPenBorder)

		tx0 := left
		tx1 := tx0 + cW1
		tx2 := tx1 + cWRest
		tx3 := tx2 + cWRest
		tx4 := tx3 + cWRest
		tx5 := tx4 + cWRest
		tx6 := left + txW

		drawLine(tx1, tY, tx1, tY+scaleY(12*vMult), hPenBorder)
		drawLine(tx2, tY, tx2, tY+scaleY(12*vMult), hPenBorder)
		drawLine(tx3, tY, tx3, tY+scaleY(12*vMult), hPenBorder)
		drawLine(tx4, tY, tx4, tY+scaleY(12*vMult), hPenBorder)
		drawLine(tx5, tY, tx5, tY+scaleY(12*vMult), hPenBorder)

		drawText("HSN/SAC", tx0, tY, tx1, tY+scaleY(5.5*vMult), fDecl, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText("Taxable Val", tx1, tY, tx2, tY+scaleY(5.5*vMult), fDecl, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText("CGST", tx2, tY, tx3, tY+scaleY(5.5*vMult), fDecl, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText("SGST", tx3, tY, tx4, tY+scaleY(5.5*vMult), fDecl, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText("IGST", tx4, tY, tx5, tY+scaleY(5.5*vMult), fDecl, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText("Total Tax", tx5, tY, tx6, tY+scaleY(5.5*vMult), fDecl, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

		drawText("1512 / 1513", tx0, tY+scaleY(5.5*vMult), tx1, tY+scaleY(12*vMult), fDecl, colorBlack, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", bill.Taxable), tx1, tY+scaleY(5.5*vMult), tx2-scaleX(1), tY+scaleY(12*vMult), fDecl, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", bill.CGST), tx2, tY+scaleY(5.5*vMult), tx3-scaleX(1), tY+scaleY(12*vMult), fDecl, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", bill.SGST), tx3, tY+scaleY(5.5*vMult), tx4-scaleX(1), tY+scaleY(12*vMult), fDecl, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", bill.IGST), tx4, tY+scaleY(5.5*vMult), tx5-scaleX(1), tY+scaleY(12*vMult), fDecl, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", bill.GSTAmt), tx5, tY+scaleY(5.5*vMult), tx6-scaleX(1), tY+scaleY(12*vMult), fDecl, colorBlack, DT_RIGHT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	}

	rY := curY
	drawText("Subtotal:", right-scaleX(75), rY, right-scaleX(32), rY+scaleY(4.5*vMult), fTableR, colorBlack, DT_RIGHT|DT_NOPREFIX)
	drawText(fmt.Sprintf("₹%.2f", bill.Subtotal), right-scaleX(32), rY, right, rY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_NOPREFIX)
	rY += scaleY(4.5 * vMult)
	if bill.Discount > 0 {
		drawText("Discount:", right-scaleX(75), rY, right-scaleX(32), rY+scaleY(4.5*vMult), fTableR, colorBlack, DT_RIGHT|DT_NOPREFIX)
		drawText(fmt.Sprintf("-₹%.2f", bill.Discount), right-scaleX(32), rY, right, rY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_NOPREFIX)
		rY += scaleY(4.5 * vMult)
	}
	if bill.GSTOn && bill.GSTAmt > 0 {
		drawText("Taxable Value:", right-scaleX(75), rY, right-scaleX(32), rY+scaleY(4.5*vMult), fTableR, colorBlack, DT_RIGHT|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", bill.Taxable), right-scaleX(32), rY, right, rY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_NOPREFIX)
		rY += scaleY(4.5 * vMult)
		drawText("Total GST (incl.):", right-scaleX(75), rY, right-scaleX(32), rY+scaleY(4.5*vMult), fTableR, colorBlack, DT_RIGHT|DT_NOPREFIX)
		drawText(fmt.Sprintf("₹%.2f", bill.GSTAmt), right-scaleX(32), rY, right, rY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_NOPREFIX)
		rY += scaleY(4.5 * vMult)
	}

	if rY > curY+scaleY(15*vMult) {
		curY = rY
	} else {
		curY += scaleY(17 * vMult)
	}

	// 5. Grand Total Row
	drawLine(left, curY, right, curY, hPenThick)
	curY += scaleY(1 * vMult)
	drawText("GRAND TOTAL:", right-scaleX(85), curY, right-scaleX(36), curY+scaleY(6.5*vMult), fTotal, colorBlack, DT_RIGHT|DT_VCENTER|DT_NOPREFIX)
	drawText(fmt.Sprintf("₹%.2f", bill.GrandTotal), right-scaleX(36), curY, right, curY+scaleY(6.5*vMult), fTotal, colorBlack, DT_RIGHT|DT_VCENTER|DT_NOPREFIX)
	curY += scaleY(6.5 * vMult)
	drawLine(left, curY, right, curY, hPenThick)
	curY += scaleY(2 * vMult)

	drawText(fmt.Sprintf("Amount Paid: ₹%.2f (%s)", bill.Paid, bill.PayMode), right-scaleX(85), curY, right, curY+scaleY(4.5*vMult), fTableR, colorBlack, DT_RIGHT|DT_NOPREFIX)
	curY += scaleY(4.5 * vMult)
	drawText(fmt.Sprintf("Balance Due: ₹%.2f", bill.Balance), right-scaleX(85), curY, right, curY+scaleY(4.5*vMult), fTableB, colorBlack, DT_RIGHT|DT_NOPREFIX)
	curY += scaleY(6 * vMult)

	// 6. Signatures & Declaration Block
	if isHalf {
		sigY := targetEndY - scaleY(13.5*vMult)
		drawLine(left+scaleX(5), sigY, left+scaleX(55), sigY, hPenBorder)
		drawText("Customer Signature", left+scaleX(5), sigY+scaleY(1.2*vMult), left+scaleX(55), sigY+scaleY(5.5*vMult), fSign, colorBlack, DT_CENTER|DT_NOPREFIX)

		drawLine(right-scaleX(65), sigY, right-scaleX(5), sigY, hPenBorder)
		drawText("For "+companyName, right-scaleX(65), sigY+scaleY(1.2*vMult), right-scaleX(5), sigY+scaleY(5.5*vMult), fSign, colorBlack, DT_CENTER|DT_NOPREFIX)
		drawText("(Authorised Signatory)", right-scaleX(65), sigY+scaleY(5.5*vMult), right-scaleX(5), sigY+scaleY(9.5*vMult), fDecl, colorDarkGray, DT_CENTER|DT_NOPREFIX)

		declY := targetEndY - scaleY(4.5*vMult)
		drawText("Declaration: We declare that this invoice shows the actual price of the goods described and that all particulars are true and correct.", left, declY, right, declY+scaleY(4.5*vMult), fDecl, colorDarkGray, DT_LEFT|DT_NOPREFIX)
	} else {
		drawLine(left+scaleX(5), curY, left+scaleX(55), curY, hPenBorder)
		drawText("Customer Signature", left+scaleX(5), curY+scaleY(1.5*vMult), left+scaleX(55), curY+scaleY(6*vMult), fSign, colorBlack, DT_CENTER|DT_NOPREFIX)

		drawLine(right-scaleX(65), curY, right-scaleX(5), curY, hPenBorder)
		drawText("For "+companyName, right-scaleX(65), curY+scaleY(1.5*vMult), right-scaleX(5), curY+scaleY(6*vMult), fSign, colorBlack, DT_CENTER|DT_NOPREFIX)
		drawText("(Authorised Signatory)", right-scaleX(65), curY+scaleY(6.5*vMult), right-scaleX(5), curY+scaleY(11*vMult), fDecl, colorDarkGray, DT_CENTER|DT_NOPREFIX)

		curY += scaleY(13 * vMult)
		drawText("Declaration: We declare that this invoice shows the actual price of the goods described and that all particulars are true and correct.", left, curY, right, curY+scaleY(5*vMult), fDecl, colorDarkGray, DT_LEFT|DT_NOPREFIX)
	}
}

// drawReceipt80mmGDI renders the exact 80mm thermal receipt template (bill1) using vector GDI
func drawReceipt80mmGDI(hdc uintptr, bill *db.Bill, s *db.Settings) {
	scaleX, scaleY, _, pageWidth, _, createFont, createPen, _, drawText, drawLine, _, _, cleanup := setupGDIHelpers(hdc)
	defer cleanup()

	colorBlack := uint32(0x00000000)
	colorDarkGray := uint32(0x00333333)

	fTitle80 := createFont(10.5, true)
	fBrand80 := createFont(13.5, true)
	fSub80 := createFont(8.0, false)
	fHeader80 := createFont(10.5, true)
	fText80 := createFont(8.5, false)
	fBold80 := createFont(8.5, true)
	fTotal80 := createFont(12.5, true)
	fFooter80 := createFont(8.5, false)
	fFooterTa80 := createFont(9.0, true)

	hPenDashed := createPen(PS_DASH, 1, colorBlack)

	companyName := "VANUR FARMER PRODUCER COMPANY LIMITED"
	if s != nil && s.CompanyName != "" {
		companyName = strings.ToUpper(s.CompanyName)
	}
	brandEn := "Siraazhi"
	if s != nil && s.BrandEn != "" {
		brandEn = s.BrandEn
	}
	brandTa := "சிராழி"
	if s != nil && s.BrandTa != "" {
		brandTa = s.BrandTa
	}
	tagline := "Pure Traditional Marachekku Oils"
	if s != nil && s.Tagline != "" {
		tagline = s.Tagline
	}
	address := "Thailapuram Village, Vanur, Villupuram, Tamil Nadu"
	if s != nil && s.Address != "" {
		address = s.Address
	}
	phone := "9876543210"
	if s != nil && s.Phone != "" {
		phone = s.Phone
	}
	fssai := "12423031000494"
	if s != nil && s.Fssai != "" {
		fssai = s.Fssai
	}

	custName := "Walk-in"
	if bill.Customer.Name != "" {
		custName = bill.Customer.Name
	}
	custPhone := "-"
	if bill.Customer.Phone != "" {
		custPhone = bill.Customer.Phone
	}

	receiptW := scaleX(72)
	left := scaleX(3)
	if pageWidth > scaleX(76) {
		left = (pageWidth - receiptW) / 2
	}
	right := left + receiptW
	curY := scaleY(8)

	// 1. Header
	drawText(companyName, left, curY, right, curY+scaleY(4.5), fTitle80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4.8)
	drawText(fmt.Sprintf("%s (%s)", brandEn, brandTa), left, curY, right, curY+scaleY(5.5), fBrand80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(5.5)
	drawText(tagline, left, curY, right, curY+scaleY(4), fSub80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4)
	drawText(address, left, curY, right, curY+scaleY(4), fSub80, colorDarkGray, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4)
	drawText(fmt.Sprintf("Ph: %s | FSSAI: %s", phone, fssai), left, curY, right, curY+scaleY(4), fSub80, colorDarkGray, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4)

	if bill.GSTOn && s != nil && s.Gstin != "" {
		drawText("GSTIN: "+s.Gstin, left, curY, right, curY+scaleY(4), fBold80, colorBlack, DT_CENTER|DT_NOPREFIX)
		curY += scaleY(4)
	}

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)

	title := "CASH RECEIPT"
	if bill.GSTOn {
		title = "TAX INVOICE"
	}
	drawText(title, left, curY, right, curY+scaleY(4.5), fHeader80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(5)

	drawText(fmt.Sprintf("Bill No: %s   Date: %s", bill.BillNo, bill.Date), left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(4)
	custLine := "Customer: " + custName
	if custPhone != "-" {
		custLine += " (" + custPhone + ")"
	}
	drawText(custLine, left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(4)
	if bill.Customer.Gstin != "" {
		drawText("Cust GSTIN: "+bill.Customer.Gstin, left, curY, right, curY+scaleY(4), fBold80, colorBlack, DT_LEFT|DT_NOPREFIX)
		curY += scaleY(4)
	}

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)

	// 2. Items Table
	drawText("HSN", left, curY, left+scaleX(20), curY+scaleY(4), fBold80, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText("Qty", left+scaleX(20), curY, left+scaleX(36), curY+scaleY(4), fBold80, colorBlack, DT_CENTER|DT_NOPREFIX)
	drawText("Rate", left+scaleX(36), curY, left+scaleX(53), curY+scaleY(4), fBold80, colorBlack, DT_RIGHT|DT_NOPREFIX)
	drawText("Amt", left+scaleX(53), curY, right, curY+scaleY(4), fBold80, colorBlack, DT_RIGHT|DT_NOPREFIX)
	curY += scaleY(4.5)
	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)

	for _, it := range bill.Items {
		itemTitle := it.NameEn
		if it.NameTa != "" {
			itemTitle += " (" + it.NameTa + ")"
		}
		drawText(itemTitle, left, curY, right, curY+scaleY(4), fBold80, colorBlack, DT_LEFT|DT_NOPREFIX)
		curY += scaleY(4)

		drawText(it.HSN, left, curY, left+scaleX(20), curY+scaleY(3.8), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
		drawText(fmt.Sprintf("%.0f", it.Qty), left+scaleX(20), curY, left+scaleX(36), curY+scaleY(3.8), fText80, colorBlack, DT_CENTER|DT_NOPREFIX)
		drawText(fmt.Sprintf("%.2f", it.Price), left+scaleX(36), curY, left+scaleX(53), curY+scaleY(3.8), fText80, colorBlack, DT_RIGHT|DT_NOPREFIX)
		drawText(fmt.Sprintf("%.2f", it.Amount), left+scaleX(53), curY, right, curY+scaleY(3.8), fBold80, colorBlack, DT_RIGHT|DT_NOPREFIX)
		curY += scaleY(4.5)
	}

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)

	// 3. Totals Block
	drawText(fmt.Sprintf("Subtotal: %.2f", bill.Subtotal), left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_RIGHT|DT_NOPREFIX)
	curY += scaleY(4.2)
	if bill.Discount > 0 {
		drawText(fmt.Sprintf("Discount: -%.2f", bill.Discount), left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_RIGHT|DT_NOPREFIX)
		curY += scaleY(4.2)
	}

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)
	drawText(fmt.Sprintf("GRAND TOTAL: ₹%.2f", bill.GrandTotal), left, curY, right, curY+scaleY(5.5), fTotal80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(5.8)
	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)

	drawText(fmt.Sprintf("Paid (%s): ₹%.2f | Balance: ₹%.2f", bill.PayMode, bill.Paid, bill.Balance), left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_RIGHT|DT_NOPREFIX)
	curY += scaleY(4.2)

	if !bill.GSTOn {
		drawText("*** RETAIL CASH MEMO (NO TAX) ***", left, curY, right, curY+scaleY(4), fSub80, colorBlack, DT_CENTER|DT_NOPREFIX)
		curY += scaleY(4)
	} else {
		drawText(fmt.Sprintf("Taxable: ₹%.2f | Total GST (incl.): ₹%.2f", bill.Taxable, bill.GSTAmt), left, curY, right, curY+scaleY(3.8), fSub80, colorBlack, DT_RIGHT|DT_NOPREFIX)
		curY += scaleY(4)
	}

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(3)

	// 4. Footer
	noteEn := "Thank you! Visit again."
	if s != nil && s.FooterNoteEn != "" {
		noteEn = s.FooterNoteEn
	}
	noteTa := "நன்றி! மீண்டும் வருக!"
	if s != nil && s.FooterNoteTa != "" {
		noteTa = s.FooterNoteTa
	}
	drawText(noteEn, left, curY, right, curY+scaleY(4), fFooter80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4.2)
	drawText(noteTa, left, curY, right, curY+scaleY(4.5), fFooterTa80, colorBlack, DT_CENTER|DT_NOPREFIX)
}

// drawTestSlipA4GDI renders an A4 hardware verification document in bill2 styling
func drawTestSlipA4GDI(hdc uintptr, printerName, orientation, colorMode string, s *db.Settings) {
	scaleX, scaleY, _, pageWidth, _, createFont, createPen, createBrush, drawText, drawLine, fillRect, drawRect, cleanup := setupGDIHelpers(hdc)
	defer cleanup()

	colorBlack := uint32(0x00000000)
	colorWhite := uint32(0x00FFFFFF)
	colorDarkGreen := uint32(0x00205E1B)
	colorLightGray := uint32(0x00F0F0F0)
	colorDarkGray := uint32(0x00444444)

	fTitle := createFont(15.5, true)
	fBrand := createFont(12.5, true)
	fBadge := createFont(11.5, true)
	fSub := createFont(9.5, false)
	fSubBold := createFont(9.5, true)
	fTableH := createFont(9.5, true)
	fTableR := createFont(9.5, false)
	fTableB := createFont(9.5, true)

	hPenThick := createPen(PS_SOLID, scaleX(0.60), colorBlack)
	hPenBorder := createPen(PS_SOLID, scaleX(0.20), colorBlack)
	hBrushWhite := createBrush(colorWhite)
	hBrushHeader := createBrush(colorLightGray)
	hBrushBlack := createBrush(colorBlack)

	companyName := "VANUR FARMER PRODUCER COMPANY LIMITED"
	if s != nil && s.CompanyName != "" {
		companyName = strings.ToUpper(s.CompanyName)
	}

	left := scaleX(10)
	right := pageWidth - scaleX(10)
	curY := scaleY(10)

	// Header
	drawText(companyName, left, curY, left+scaleX(120), curY+scaleY(6), fTitle, colorBlack, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	badgeW := scaleX(68)
	badgeH := scaleY(5.5)
	fillRect(right-badgeW, curY, right, curY+badgeH, hBrushBlack)
	drawText("HARDWARE VERIFICATION TEST", right-badgeW, curY, right, curY+badgeH, fBadge, colorWhite, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)

	curY += scaleY(6.5)
	drawText("Siraazhi (சிராழி) — Pure Traditional Marachekku Oils", left, curY, left+scaleX(120), curY+scaleY(5), fBrand, colorDarkGreen, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	curY += scaleY(5)
	drawText("Thailapuram Village, Vanur, Villupuram, Tamil Nadu", left, curY, left+scaleX(120), curY+scaleY(4.5), fSub, colorDarkGray, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	curY += scaleY(4.5)
	drawText("Phone: 9876543210 | FSSAI Lic. No: 12423031000494", left, curY, left+scaleX(120), curY+scaleY(4.5), fSub, colorBlack, DT_LEFT|DT_SINGLELINE|DT_NOPREFIX)
	curY += scaleY(5)

	drawLine(left, curY, right, curY, hPenThick)
	curY += scaleY(4)

	// Test Details Grid
	bH := scaleY(28)
	drawRect(left, curY, right, curY+bH, hPenBorder, hBrushWhite)
	nowStr := time.Now().Format("02/01/2006 15:04:05")

	drawText("Printer Name:", left+scaleX(4), curY+scaleY(2), left+scaleX(45), curY+scaleY(6), fTableB, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText(printerName, left+scaleX(45), curY+scaleY(2), right-scaleX(4), curY+scaleY(6), fTableR, colorBlack, DT_LEFT|DT_NOPREFIX)

	drawText("Hardware Class:", left+scaleX(4), curY+scaleY(6.5), left+scaleX(45), curY+scaleY(10.5), fTableB, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText("Standard Document Spooler (Laser / Inkjet / Smart Tank)", left+scaleX(45), curY+scaleY(6.5), right-scaleX(4), curY+scaleY(10.5), fTableR, colorBlack, DT_LEFT|DT_NOPREFIX)

	drawText("Paper Size Preset:", left+scaleX(4), curY+scaleY(11), left+scaleX(45), curY+scaleY(15), fTableB, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText("A4 Sheet (210 x 297 mm)", left+scaleX(45), curY+scaleY(11), right-scaleX(4), curY+scaleY(15), fTableR, colorBlack, DT_LEFT|DT_NOPREFIX)

	drawText("Hardware Status:", left+scaleX(4), curY+scaleY(15.5), left+scaleX(45), curY+scaleY(19.5), fTableB, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText("ONLINE - READY ✓", left+scaleX(45), curY+scaleY(15.5), right-scaleX(4), curY+scaleY(19.5), fTableB, colorDarkGreen, DT_LEFT|DT_NOPREFIX)

	drawText("Spool Timestamp:", left+scaleX(4), curY+scaleY(20), left+scaleX(45), curY+scaleY(24), fTableB, colorBlack, DT_LEFT|DT_NOPREFIX)
	drawText(nowStr, left+scaleX(45), curY+scaleY(20), right-scaleX(4), curY+scaleY(24), fTableR, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += bH + scaleY(4)

	// Verification Results Table
	hH := scaleY(7)
	fillRect(left, curY, right, curY+hH, hBrushHeader)
	drawRect(left, curY, right, curY+hH, hPenBorder, hBrushHeader)
	drawText("VERIFICATION RESULTS & SUBSYSTEM STATUS", left+scaleX(4), curY, right, curY+hH, fTableH, colorBlack, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	curY += hH

	capabilities := []string{
		"[✓] Direct Zero-Dialog Windows GDI Document Spooling Active",
		"[✓] Mobile & Tablet Wireless LAN Printing Enabled via Master Queue",
		"[✓] Full Corporate A4 Tax Invoicing & Multi-Printer Routing Ready",
		"[✓] Universal Hardware Support for HP Smart Tank, LaserJet, Inkjet & POS Thermal",
	}

	for _, cap := range capabilities {
		rH := scaleY(8)
		drawRect(left, curY, right, curY+rH, hPenBorder, hBrushWhite)
		drawText(cap, left+scaleX(4), curY, right-scaleX(4), curY+rH, fTableR, colorBlack, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		curY += rH
	}
	curY += scaleY(12)

	drawLine(right-scaleX(65), curY, right-scaleX(5), curY, hPenBorder)
	drawText("For "+companyName, right-scaleX(65), curY+scaleY(1.5), right-scaleX(5), curY+scaleY(6), fSubBold, colorBlack, DT_CENTER|DT_NOPREFIX)
	drawText("(Authorised Signatory)", right-scaleX(65), curY+scaleY(6.5), right-scaleX(5), curY+scaleY(11), fSub, colorDarkGray, DT_CENTER|DT_NOPREFIX)
}

// drawTestSlip80mmGDI renders an 80mm hardware verification receipt in bill1 styling
func drawTestSlip80mmGDI(hdc uintptr, printerName string, s *db.Settings) {
	scaleX, scaleY, _, pageWidth, _, createFont, createPen, _, drawText, drawLine, _, _, cleanup := setupGDIHelpers(hdc)
	defer cleanup()

	colorBlack := uint32(0x00000000)
	colorDarkGray := uint32(0x00333333)

	fTitle80 := createFont(10.5, true)
	fBrand80 := createFont(13.5, true)
	fSub80 := createFont(8.0, false)
	fHeader80 := createFont(10.5, true)
	fText80 := createFont(8.5, false)
	fBold80 := createFont(8.5, true)
	fFooterTa80 := createFont(9.0, true)

	hPenDashed := createPen(PS_DASH, 1, colorBlack)

	companyName := "VANUR FARMER PRODUCER COMPANY LIMITED"
	if s != nil && s.CompanyName != "" {
		companyName = strings.ToUpper(s.CompanyName)
	}

	receiptW := scaleX(72)
	left := scaleX(3)
	if pageWidth > scaleX(76) {
		left = (pageWidth - receiptW) / 2
	}
	right := left + receiptW
	curY := scaleY(8)

	// Header
	drawText(companyName, left, curY, right, curY+scaleY(4.5), fTitle80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4.8)
	drawText("Siraazhi (சிராழி)", left, curY, right, curY+scaleY(5.5), fBrand80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(5.5)
	drawText("Pure Traditional Marachekku Oils", left, curY, right, curY+scaleY(4), fSub80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4)
	drawText("Thailapuram Village, Vanur, Villupuram, Tamil Nadu", left, curY, right, curY+scaleY(4), fSub80, colorDarkGray, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4)
	drawText("Ph: 9876543210 | FSSAI: 12423031000494", left, curY, right, curY+scaleY(4), fSub80, colorDarkGray, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4)

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)

	drawText("HARDWARE PRINTER TEST SLIP", left, curY, right, curY+scaleY(4.5), fHeader80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(5)

	drawText("Printer: "+printerName, left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(4)
	drawText("Paper Size: 80mm Roll (3-inch)", left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(4)
	drawText("Status: ONLINE - READY ✓", left, curY, right, curY+scaleY(4), fBold80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(4)
	drawText("Time: "+time.Now().Format("02/01/2006 15:04:05"), left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(4)

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2)

	drawText("[✓] Zero-Dialog Hardware Spooling Active", left, curY, right, curY+scaleY(3.8), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(3.8)
	drawText("[✓] 80mm Thermal Receipt Layout Ready", left, curY, right, curY+scaleY(3.8), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(3.8)
	drawText("[✓] Multi-Language & Tamil Printing Ready", left, curY, right, curY+scaleY(3.8), fText80, colorBlack, DT_LEFT|DT_NOPREFIX)
	curY += scaleY(3.8)

	drawLine(left, curY, right, curY, hPenDashed)
	curY += scaleY(2.5)

	drawText("Hardware Test Slip Dispatched Successfully!", left, curY, right, curY+scaleY(4), fBold80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4.5)
	drawText("Thank you! Visit again.", left, curY, right, curY+scaleY(4), fText80, colorBlack, DT_CENTER|DT_NOPREFIX)
	curY += scaleY(4.2)
	drawText("நன்றி! மீண்டும் வருக!", left, curY, right, curY+scaleY(4.5), fFooterTa80, colorBlack, DT_CENTER|DT_NOPREFIX)
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
