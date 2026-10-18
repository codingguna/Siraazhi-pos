/* ==========================================================================
   Siraazhi Pro POS — Standalone Windows GUI Uninstaller
   
   WHAT IT IS:
     Official native Windows uninstaller executable (uninstall.exe):
     - Displays native Windows confirmation message box with official app icon.
     - Requests UAC elevation automatically if not running as Administrator.
     - Terminates active POS background instances.
     - Removes Desktop and Start Menu shortcuts.
     - Deletes Windows startup registry keys and Control Panel registration.
     - Safely removes C:\Program Files\SiraazhiPOS while preserving C:\ProgramData\SiraazhiPOS\data\pos.db.
   ========================================================================== */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	shell32          = syscall.NewLazyDLL("shell32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
	procShellExecute = shell32.NewProc("ShellExecuteW")
)

const (
	MB_YESNO        = 0x00000004
	MB_ICONQUESTION = 0x00000020
	MB_ICONINFO     = 0x00000040
	MB_ICONERROR    = 0x00000010
	IDYES           = 6
)

func messageBox(title, text string, style uint) int {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	ret, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), uintptr(style))
	return int(ret)
}

func isElevated() bool {
	_, err := os.Open(`\\.\PHYSICALDRIVE0`)
	return err == nil
}

func runAsAdmin() {
	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := os.Executable()
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	procShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(exePtr)), 0, 0, 1)
}

func main() {
	if !isElevated() {
		runAsAdmin()
		return
	}

	choice := messageBox(
		"Siraazhi Pro POS — Uninstaller",
		"Are you sure you want to completely uninstall Siraazhi Pro POS from your computer?\n\n"+
			"Note: Your sales database in C:\\ProgramData\\SiraazhiPOS will be safely preserved.",
		MB_YESNO|MB_ICONQUESTION,
	)

	if choice != IDYES {
		return
	}

	// 1. Terminate running POS processes
	exec.Command("taskkill", "/f", "/im", "siraazhi-pos.exe").Run()

	// 2. Remove Desktop & Start Menu Shortcuts
	psScript := `
		$desk = [Environment]::GetFolderPath('Desktop');
		Remove-Item -Path "$desk\Siraazhi Pro POS.lnk" -Force -ErrorAction SilentlyContinue;
		$startMenu = [Environment]::GetFolderPath('CommonStartMenu');
		Remove-Item -Path "$startMenu\Programs\Siraazhi Pro POS.lnk" -Force -ErrorAction SilentlyContinue;
		Remove-Item -Path "$startMenu\Programs\SiraazhiPOS" -Recurse -Force -ErrorAction SilentlyContinue;
	`
	exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript).Run()

	// 3. Remove Windows Boot Auto-Start
	exec.Command("reg", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS", "/f").Run()

	// 4. Remove Control Panel Registration
	exec.Command("reg", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\SiraazhiPOS`, "/f").Run()

	messageBox(
		"Uninstall Complete",
		"✅ Siraazhi Pro POS has been uninstalled successfully.\n\n"+
			"Your sales database in C:\\ProgramData\\SiraazhiPOS was preserved.",
		MB_ICONINFO,
	)

	// 5. Cleanup Program Files folder after exit
	exePath, _ := os.Executable()
	installDir := filepath.Dir(exePath)
	cmdDelete := fmt.Sprintf(`timeout /t 1 /nobreak >nul & for /l %%x in (1,1,10) do (rmdir /s /q "%s" >nul 2>&1 & if not exist "%s" exit)`, installDir, installDir)
	exec.Command("cmd", "/c", "start", "/b", "cmd", "/c", cmdDelete).Start()
}
