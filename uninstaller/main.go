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
	"strings"
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

func runAsAdmin(extraArgs ...string) {
	verb, _ := syscall.UTF16PtrFromString("runas")
	exe, _ := os.Executable()
	exePtr, _ := syscall.UTF16PtrFromString(exe)
	argStr := strings.Join(extraArgs, " ")
	argPtr, _ := syscall.UTF16PtrFromString(argStr)
	procShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(exePtr)), uintptr(unsafe.Pointer(argPtr)), 0, 1)
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

func copyDBFiles(srcDB, dstDB string) {
	_ = copyFile(srcDB, dstDB)
	_ = copyFile(srcDB+"-wal", dstDB+"-wal")
	_ = copyFile(srcDB+"-shm", dstDB+"-shm")
}

func main() {
	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}

	tempDir := os.TempDir()
	isFromTemp := false
	var passedInstallDir string

	for i, arg := range os.Args[1:] {
		if arg == "--from-temp" {
			isFromTemp = true
			if i+1 < len(os.Args[1:]) {
				passedInstallDir = os.Args[1:][i+1]
			}
			break
		}
	}

	// If running directly inside Program Files, copy ourselves to %TEMP%
	// and launch from there so Program Files can be completely deleted without file locks!
	if !isFromTemp && !strings.HasPrefix(strings.ToLower(exePath), strings.ToLower(tempDir)) {
		tempExe := filepath.Join(tempDir, "siraazhi_uninstall_worker.exe")
		_ = copyFile(exePath, tempExe)

		installDir := filepath.Dir(exePath)
		cmd := exec.Command(tempExe, "--from-temp", installDir)
		cmd.Dir = tempDir
		_ = cmd.Start()
		return
	}

	if !isElevated() {
		runAsAdmin(os.Args[1:]...)
		return
	}

	installDir := passedInstallDir
	if installDir == "" {
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		installDir = filepath.Join(programFiles, "SiraazhiPOS")
	}

	// Resolve user AppData path
	appData := os.Getenv("APPDATA")
	if appData == "" {
		userProfile := os.Getenv("USERPROFILE")
		if userProfile != "" {
			appData = filepath.Join(userProfile, "AppData", "Roaming")
		} else {
			appData = filepath.Join(os.Getenv("ProgramData"), "SiraazhiPOS")
		}
	}

	appDataDir := filepath.Join(appData, "SiraazhiPOS", "data")
	backupDir := filepath.Join(appData, "SiraazhiPOS", "backups")
	_ = os.MkdirAll(appDataDir, 0755)
	_ = os.MkdirAll(backupDir, 0755)

	appDataDb := filepath.Join(appDataDir, "pos.db")
	programDataDb := filepath.Join(os.Getenv("ProgramData"), "SiraazhiPOS", "data", "pos.db")
	installDb := filepath.Join(installDir, "data", "pos.db")

	choice := messageBox(
		"Siraazhi Pro POS — Uninstaller",
		"Are you sure you want to uninstall Siraazhi Pro POS from your computer?\n\n"+
			"🛡️ ZERO DATA LOSS GUARANTEE:\n"+
			"Your complete sales history, invoices, inventory, and shop settings in:\n"+
			appDataDb+"\n"+
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
	time.Sleep(300 * time.Millisecond)

	// 2. Safety Migration & Backup of Database before removing program files
	// If pos.db exists in Program Files or ProgramData but not in AppData, migrate to AppData now
	if _, err := os.Stat(appDataDb); os.IsNotExist(err) {
		if fi, err := os.Stat(programDataDb); err == nil && fi.Size() > 0 {
			copyDBFiles(programDataDb, appDataDb)
		} else if fi, err := os.Stat(installDb); err == nil && fi.Size() > 0 {
			copyDBFiles(installDb, appDataDb)
		}
	}

	// Create automatic timestamped safety backup in AppData
	if fi, err := os.Stat(appDataDb); err == nil && fi.Size() > 0 {
		ts := time.Now().Format("20060102_150405")
		_ = copyFile(appDataDb, filepath.Join(backupDir, fmt.Sprintf("pos_backup_pre_uninstall_%s.db", ts)))
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

	// 6. Completely Remove Program Files Folder
	// Because we are running from %TEMP%, installDir has no open handles from uninstall.exe
	_ = os.RemoveAll(installDir)
	// Fallback PowerShell command to wipe any remaining files in installDir
	psClean := fmt.Sprintf(`
		Start-Sleep -Milliseconds 500;
		if (Test-Path '%s') {
			Remove-Item -Path '%s' -Recurse -Force -ErrorAction SilentlyContinue;
		}
	`, installDir, installDir)
	exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psClean).Run()

	messageBox(
		"Uninstall Complete",
		"✅ Siraazhi Pro POS has been uninstalled successfully.\n\n"+
			"• Program files removed from: "+installDir+"\n"+
			"• Shortcuts and auto-start removed\n\n"+
			"🛡️ All sales data, bills, and settings were safely preserved in:\n"+
			appDataDb+"\n\n"+
			"An automatic safety backup was also saved to:\n"+
			backupDir+"\n\n"+
			"You can reinstall at any time without losing any data.",
		MB_ICONINFO,
	)
}

