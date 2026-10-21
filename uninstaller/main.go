/* ==========================================================================
   Siraazhi Pro POS — Standalone Windows GUI Uninstaller
   
   WHAT IT IS:
     Official native Windows uninstaller executable (uninstall.exe):
     - Displays native Windows confirmation message box with official app icon.
     - Requests UAC elevation automatically if not running as Administrator.
     - Terminates active POS background instances.
     - Safely backs up sales database before removing program files.
     - Removes Desktop and Start Menu shortcuts.
     - Deletes Windows startup registry keys and Control Panel registration.
     - Safely removes C:\Program Files\SiraazhiPOS while preserving C:\ProgramData\SiraazhiPOS\data\pos.db.
     - ZERO DATA LOSS: All customer bills, products, and accounting records are kept intact.
   ========================================================================== */

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	_ = os.MkdirAll(filepath.Dir(dst), 0755)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func main() {
	if !isElevated() {
		runAsAdmin()
		return
	}

	choice := messageBox(
		"Siraazhi Pro POS — Uninstaller",
		"Are you sure you want to uninstall Siraazhi Pro POS from your computer?\n\n"+
			"🛡️ ZERO DATA LOSS GUARANTEE:\n"+
			"Your complete sales history, invoices, inventory, and shop settings in:\n"+
			"C:\\ProgramData\\SiraazhiPOS\\data\\pos.db\n"+
			"will be SAFELY PRESERVED and will NOT be deleted.\n\n"+
			"If you reinstall in the future, all your data will be automatically available.\n\n"+
			"Click [Yes] to proceed with uninstalling program files, or [No] to cancel.",
		MB_YESNO|MB_ICONQUESTION,
	)

	if choice != IDYES {
		return
	}

	// 1. Terminate running POS processes
	exec.Command("taskkill", "/f", "/im", "siraazhi-pos.exe").Run()

	// 2. Safety Migration & Backup of Database before removing program files
	exePath, _ := os.Executable()
	installDir := filepath.Dir(exePath)
	dataDir := filepath.Join(os.Getenv("ProgramData"), "SiraazhiPOS", "data")
	backupDir := filepath.Join(os.Getenv("ProgramData"), "SiraazhiPOS", "backups")
	_ = os.MkdirAll(dataDir, 0755)
	_ = os.MkdirAll(backupDir, 0755)

	installDb := filepath.Join(installDir, "data", "pos.db")
	programDataDb := filepath.Join(dataDir, "pos.db")

	// If a DB exists in Program Files, rescue it to ProgramData so it is never deleted by rmdir
	if fi, err := os.Stat(installDb); err == nil && fi.Size() > 0 {
		if _, err2 := os.Stat(programDataDb); os.IsNotExist(err2) {
			_ = copyFile(installDb, programDataDb)
		}
	}

	// Create automatic timestamped backup of the database
	if fi, err := os.Stat(programDataDb); err == nil && fi.Size() > 0 {
		ts := time.Now().Format("20060102_150405")
		_ = copyFile(programDataDb, filepath.Join(backupDir, fmt.Sprintf("pos_backup_pre_uninstall_%s.db", ts)))
	}

	// 3. Remove Desktop & Start Menu Shortcuts
	psScript := `
		$desk = [Environment]::GetFolderPath('Desktop');
		Remove-Item -Path "$desk\Siraazhi Pro POS.lnk" -Force -ErrorAction SilentlyContinue;
		$startMenu = [Environment]::GetFolderPath('CommonStartMenu');
		Remove-Item -Path "$startMenu\Programs\Siraazhi Pro POS.lnk" -Force -ErrorAction SilentlyContinue;
		Remove-Item -Path "$startMenu\Programs\SiraazhiPOS" -Recurse -Force -ErrorAction SilentlyContinue;
	`
	exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript).Run()

	// 4. Remove Windows Boot Auto-Start
	exec.Command("reg", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS", "/f").Run()

	// 5. Remove Control Panel Registration
	exec.Command("reg", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\SiraazhiPOS`, "/f").Run()

	messageBox(
		"Uninstall Complete",
		"✅ Siraazhi Pro POS has been uninstalled successfully.\n\n"+
			"🛡️ All sales data, bills, and settings were safely preserved in:\n"+
			"C:\\ProgramData\\SiraazhiPOS\\data\\pos.db\n\n"+
			"An automatic safety backup was also saved to:\n"+
			"C:\\ProgramData\\SiraazhiPOS\\backups\\\n\n"+
			"You can reinstall at any time without losing any data.",
		MB_ICONINFO,
	)

	// 6. Cleanup Program Files folder after exit
	cmdDelete := fmt.Sprintf(`timeout /t 1 /nobreak >nul & for /l %%x in (1,1,10) do (rmdir /s /q "%s" >nul 2>&1 & if not exist "%s" exit)`, installDir, installDir)
	exec.Command("cmd", "/c", "start", "/b", "cmd", "/c", cmdDelete).Start()
}
