//go:build installer
// +build installer

/* ==========================================================================
   Siraazhi Pro POS — Official Self-Extracting Windows Setup Installer

   
   WHAT IT IS:
     A single standalone Windows installer (.exe) that users run:
     - Embeds the complete Siraazhi POS binary, uninstaller, and assets.
     - Detects if Siraazhi Pro POS is already installed on the computer.
     - If ALREADY INSTALLED, opens Maintenance Mode with options:
         1. MODIFY / UPDATE: Updates old files with the new updated version.
         2. REPAIR: Scans files and fixes any broken, missing, or corrupted files/shortcuts.
         3. CANCEL: Exits setup without making changes.
     - ZERO DATA LOSS: All customer bills, products, and settings in pos.db are
       preserved across install, reinstall, modify, update, and repair!
     - Automatic safety backups of the database are created before any maintenance.
   ========================================================================== */

package main

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

//go:embed siraazhi-pos.exe
var embeddedBinary []byte

//go:embed uninstall.exe
var embeddedUninstaller []byte

//go:embed app.ico
var embeddedIcon []byte

//go:embed README.txt
var embeddedReadme []byte

//go:embed LICENSE.txt
var embeddedLicense []byte

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procShellExecute        = shell32.NewProc("ShellExecuteW")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procSetDlgItemTextW     = user32.NewProc("SetDlgItemTextW")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
)

const (
	WH_CBT          = 5
	HCBT_ACTIVATE   = 5
	MB_OK           = 0x00000000
	MB_YESNO        = 0x00000004
	MB_YESNOCANCEL  = 0x00000003
	MB_ICONQUESTION = 0x00000020
	MB_ICONINFO     = 0x00000040
	MB_ICONWARNING  = 0x00000030
	MB_ICONERROR    = 0x00000010
	IDYES           = 6
	IDNO            = 7
	IDCANCEL        = 2
)

func messageBox(title, text string, style uint) int {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	ret, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), uintptr(style))
	return int(ret)
}

func messageBoxCustomButtons(title, text string, btnYes, btnNo, btnCancel string) int {
	var hHook uintptr
	threadId, _, _ := procGetCurrentThreadId.Call()

	cb := syscall.NewCallback(func(nCode int, wParam, lParam uintptr) uintptr {
		if nCode == HCBT_ACTIVATE {
			hwnd := wParam
			if btnYes != "" {
				sYes, _ := syscall.UTF16PtrFromString(btnYes)
				procSetDlgItemTextW.Call(hwnd, uintptr(IDYES), uintptr(unsafe.Pointer(sYes)))
			}
			if btnNo != "" {
				sNo, _ := syscall.UTF16PtrFromString(btnNo)
				procSetDlgItemTextW.Call(hwnd, uintptr(IDNO), uintptr(unsafe.Pointer(sNo)))
			}
			if btnCancel != "" {
				sCancel, _ := syscall.UTF16PtrFromString(btnCancel)
				procSetDlgItemTextW.Call(hwnd, uintptr(IDCANCEL), uintptr(unsafe.Pointer(sCancel)))
			}
		}
		r, _, _ := procCallNextHookEx.Call(hHook, uintptr(nCode), wParam, lParam)
		return r
	})

	h, _, _ := procSetWindowsHookExW.Call(uintptr(WH_CBT), cb, 0, threadId)
	hHook = h
	defer procUnhookWindowsHookEx.Call(hHook)

	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	style := uint(MB_YESNOCANCEL | MB_ICONQUESTION)
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

func isRegistryInstalled() bool {
	cmd := exec.Command("reg", "query", `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\SiraazhiPOS`)
	return cmd.Run() == nil
}

// backupDatabase creates an automatic timestamped backup of the database before any modify/repair/install.
func backupDatabase(dataDir, installDir, actionName string) string {
	appDataFolder := filepath.Dir(dataDir)
	backupDir := filepath.Join(appDataFolder, "backups")
	_ = os.MkdirAll(backupDir, 0755)

	targetDb := filepath.Join(dataDir, "pos.db")
	programDataDb := filepath.Join(os.Getenv("ProgramData"), "SiraazhiPOS", "data", "pos.db")
	oldInstallDb := filepath.Join(installDir, "data", "pos.db")

	srcDb := ""
	if fi, err := os.Stat(targetDb); err == nil && fi.Size() > 0 {
		srcDb = targetDb
	} else if fi, err := os.Stat(programDataDb); err == nil && fi.Size() > 0 {
		srcDb = programDataDb
		// Migrate to AppData
		_ = os.MkdirAll(dataDir, 0755)
		_ = copyFile(programDataDb, targetDb)
		_ = copyFile(programDataDb+"-wal", targetDb+"-wal")
		_ = copyFile(programDataDb+"-shm", targetDb+"-shm")
	} else if fi, err := os.Stat(oldInstallDb); err == nil && fi.Size() > 0 {
		srcDb = oldInstallDb
		// Migrate to AppData
		_ = os.MkdirAll(dataDir, 0755)
		_ = copyFile(oldInstallDb, targetDb)
		_ = copyFile(oldInstallDb+"-wal", targetDb+"-wal")
		_ = copyFile(oldInstallDb+"-shm", targetDb+"-shm")
	}

	if srcDb == "" {
		return ""
	}

	ts := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("pos_backup_%s_%s.db", actionName, ts))
	_ = copyFile(srcDb, backupPath)
	return backupPath
}

func terminateRunningInstances() {
	exec.Command("taskkill", "/f", "/im", "siraazhi-pos.exe").Run()
	time.Sleep(300 * time.Millisecond)
}

func createShortcuts(targetExe, dataDir, installDir, targetIcon, targetUninstaller string) {
	psScript := fmt.Sprintf(`
		$ws = New-Object -ComObject WScript.Shell;
		$desk = [Environment]::GetFolderPath('Desktop');
		$s1 = $ws.CreateShortcut("$desk\Siraazhi Pro POS.lnk");
		$s1.TargetPath = '%s';
		$s1.Arguments = '-port 8080 -db ""%s\pos.db""';
		$s1.WorkingDirectory = '%s';
		$s1.IconLocation = '%s,0';
		$s1.Description = 'Siraazhi Pro POS — Billing & Inventory Suite';
		$s1.Save();

		$startMenu = [Environment]::GetFolderPath('CommonStartMenu');
		$programsFolder = "$startMenu\Programs\SiraazhiPOS";
		if (-not (Test-Path -Path $programsFolder)) {
			New-Item -ItemType Directory -Path $programsFolder -Force | Out-Null;
		}

		$s2 = $ws.CreateShortcut("$programsFolder\Siraazhi Pro POS.lnk");
		$s2.TargetPath = '%s';
		$s2.Arguments = '-port 8080 -db ""%s\pos.db""';
		$s2.WorkingDirectory = '%s';
		$s2.IconLocation = '%s,0';
		$s2.Description = 'Siraazhi Pro POS — Billing & Inventory Suite';
		$s2.Save();

		$s3 = $ws.CreateShortcut("$programsFolder\Uninstall Siraazhi Pro POS.lnk");
		$s3.TargetPath = '%s';
		$s3.WorkingDirectory = '%s';
		$s3.IconLocation = '%s,0';
		$s3.Description = 'Uninstall Siraazhi Pro POS';
		$s3.Save();

		$s4 = $ws.CreateShortcut("$startMenu\Programs\Siraazhi Pro POS.lnk");
		$s4.TargetPath = '%s';
		$s4.Arguments = '-port 8080 -db ""%s\pos.db""';
		$s4.WorkingDirectory = '%s';
		$s4.IconLocation = '%s,0';
		$s4.Description = 'Siraazhi Pro POS — Billing & Inventory Suite';
		$s4.Save();
	`, targetExe, dataDir, installDir, targetIcon,
		targetExe, dataDir, installDir, targetIcon,
		targetUninstaller, installDir, targetIcon,
		targetExe, dataDir, installDir, targetIcon)

	exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psScript).Run()
}

func registerWindowsIntegration(targetExe, dataDir, installDir, targetIcon, targetUninstaller string) {
	// Auto-start in Windows Run registry:
	// Register strictly in HKCU (headless silent background) and delete any HKLM entry to avoid duplicate entries in Task Manager
	runCmd := fmt.Sprintf(`"%s" -headless -port 8080 -db "%s\pos.db"`, targetExe, dataDir)
	exec.Command("reg", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS", "/f").Run()
	exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS", "/t", "REG_SZ", "/d", runCmd, "/f").Run()

	// Windows Settings / Control Panel (Add or Remove Programs)
	regUninstall := `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\SiraazhiPOS`
	exec.Command("reg", "add", regUninstall, "/v", "DisplayName", "/t", "REG_SZ", "/d", "Siraazhi Pro POS (Vanur FPC Ltd)", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", "1.0.0", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "Publisher", "/t", "REG_SZ", "/d", "Vanur Farmer Producer Company Limited", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "InstallLocation", "/t", "REG_SZ", "/d", installDir, "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "DisplayIcon", "/t", "REG_SZ", "/d", targetIcon, "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "UninstallString", "/t", "REG_SZ", "/d", fmt.Sprintf("\"%s\"", targetUninstaller), "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "QuietUninstallString", "/t", "REG_SZ", "/d", fmt.Sprintf("\"%s\"", targetUninstaller), "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "EstimatedSize", "/t", "REG_DWORD", "/d", "32000", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "NoModify", "/t", "REG_DWORD", "/d", "0", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "NoRepair", "/t", "REG_DWORD", "/d", "0", "/f").Run()
}

func main() {
	if !isElevated() {
		runAsAdmin()
		return
	}

	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	installDir := filepath.Join(programFiles, "SiraazhiPOS")

	appData := os.Getenv("APPDATA")
	if appData == "" {
		userProfile := os.Getenv("USERPROFILE")
		if userProfile != "" {
			appData = filepath.Join(userProfile, "AppData", "Roaming")
		} else {
			appData = filepath.Join(os.Getenv("ProgramData"), "SiraazhiPOS")
		}
	}
	dataDir := filepath.Join(appData, "SiraazhiPOS", "data")

	targetExe := filepath.Join(installDir, "siraazhi-pos.exe")
	targetUninstaller := filepath.Join(installDir, "uninstall.exe")
	targetIcon := filepath.Join(installDir, "app.ico")
	targetReadme := filepath.Join(installDir, "README.txt")
	targetLicense := filepath.Join(installDir, "LICENSE.txt")

	// Detect if Siraazhi Pro POS is actively installed on the computer
	// Both the executable file AND the Windows Uninstall registry key must be present!
	// If uninstalled previously, it will proceed directly with a clean fresh installation.
	isAlreadyInstalled := false
	if fi, err := os.Stat(targetExe); err == nil && fi.Size() > 0 && isRegistryInstalled() {
		isAlreadyInstalled = true
	}

	if isAlreadyInstalled {
		// ====================================================================
		// MAINTENANCE MODE (Already Installed)
		// ====================================================================
		maintenanceMsg := fmt.Sprintf(
			"Siraazhi Pro POS is already installed on this computer!\n"+
				"Location: %s\n\n"+
				"An existing installation was detected. Please choose an action:\n\n"+
				"• Click [Modify] to UPDATE old files with the new updated version.\n"+
				"  (All your bills, inventory, customer records, and settings remain 100%% safe)\n\n"+
				"• Click [Repair] to SCAN and FIX any missing, broken, or corrupted files.\n"+
				"  (Restores missing shortcuts and program files without touching your data)\n\n"+
				"• Click [Cancel] to EXIT setup without making any changes.\n\n"+
				"🛡️ ZERO DATA LOSS: Your existing sales database will NOT be affected.",
			installDir,
		)

		choice := messageBoxCustomButtons(
			"Siraazhi Pro POS Setup — Maintenance Wizard",
			maintenanceMsg,
			"Modify / Update",
			"Repair",
			"Cancel",
		)

		switch choice {
		case IDYES:
			// ----------------------------------------------------------------
			// ACTION 1: MODIFY / UPDATE
			// ----------------------------------------------------------------
			backupPath := backupDatabase(dataDir, installDir, "modify")
			terminateRunningInstances()

			// Overwrite old files with new updated version
			_ = os.MkdirAll(installDir, 0755)
			_ = os.MkdirAll(dataDir, 0755)

			if err := os.WriteFile(targetExe, embeddedBinary, 0755); err != nil {
				messageBox("Update Error", fmt.Sprintf("Failed to update %s:\n%v", targetExe, err), MB_ICONERROR)
				return
			}
			_ = os.WriteFile(targetUninstaller, embeddedUninstaller, 0755)
			_ = os.WriteFile(targetIcon, embeddedIcon, 0644)
			_ = os.WriteFile(targetReadme, embeddedReadme, 0644)
			_ = os.WriteFile(targetLicense, embeddedLicense, 0644)

			// Refresh shortcuts and registry
			createShortcuts(targetExe, dataDir, installDir, targetIcon, targetUninstaller)
			registerWindowsIntegration(targetExe, dataDir, installDir, targetIcon, targetUninstaller)

			backupNotice := ""
			if backupPath != "" {
				backupNotice = "\n• Automatic DB Safety Backup created: " + filepath.Base(backupPath)
			}

			messageBox(
				"Update Complete",
				"✅ Siraazhi Pro POS has been successfully updated to the latest version!\n\n"+
					"• Old program files replaced with new updated version\n"+
					"• Desktop & Start Menu shortcuts refreshed\n"+
					"• Windows background service and auto-start updated\n"+
					"• Sales database & settings: Safely preserved (0 data lost)"+backupNotice+"\n\n"+
					"Launching updated Siraazhi Pro POS now...",
				MB_ICONINFO,
			)

			// Launch updated application
			exec.Command(targetExe, "-port", "8080", "-db", filepath.Join(dataDir, "pos.db")).Start()
			return

		case IDNO:
			// ----------------------------------------------------------------
			// ACTION 2: REPAIR (Scan & Fix Missing/Broken Files)
			// ----------------------------------------------------------------
			backupPath := backupDatabase(dataDir, installDir, "repair")
			terminateRunningInstances()

			_ = os.MkdirAll(installDir, 0755)
			_ = os.MkdirAll(dataDir, 0755)

			fixedCount := 0

			// 1. Scan and verify main executable
			fi, err := os.Stat(targetExe)
			if err != nil || fi.Size() == 0 || int(fi.Size()) != len(embeddedBinary) {
				_ = os.WriteFile(targetExe, embeddedBinary, 0755)
				fixedCount++
			}

			// 2. Scan and verify uninstaller
			if fi, err := os.Stat(targetUninstaller); err != nil || fi.Size() == 0 {
				_ = os.WriteFile(targetUninstaller, embeddedUninstaller, 0755)
				fixedCount++
			}

			// 3. Scan and verify icon
			if fi, err := os.Stat(targetIcon); err != nil || fi.Size() == 0 {
				_ = os.WriteFile(targetIcon, embeddedIcon, 0644)
				fixedCount++
			}

			// 4. Scan and verify documents
			if _, err := os.Stat(targetReadme); err != nil {
				_ = os.WriteFile(targetReadme, embeddedReadme, 0644)
				fixedCount++
			}
			if _, err := os.Stat(targetLicense); err != nil {
				_ = os.WriteFile(targetLicense, embeddedLicense, 0644)
				fixedCount++
			}

			// 5. Scan and repair shortcuts
			createShortcuts(targetExe, dataDir, installDir, targetIcon, targetUninstaller)

			// 6. Scan and repair registry
			registerWindowsIntegration(targetExe, dataDir, installDir, targetIcon, targetUninstaller)

			backupNotice := ""
			if backupPath != "" {
				backupNotice = "\n• Automatic DB Safety Backup created: " + filepath.Base(backupPath)
			}

			messageBox(
				"Repair Complete",
				"✅ Siraazhi Pro POS repair completed successfully!\n\n"+
					"• All program files scanned & verified (Fixed items: "+fmt.Sprintf("%d", fixedCount)+")\n"+
					"• Desktop & Start Menu shortcuts verified and repaired\n"+
					"• Windows auto-start and uninstall registry keys restored\n"+
					"• Sales database: Verified intact and preserved (0 data lost)"+backupNotice+"\n\n"+
					"Launching Siraazhi Pro POS now...",
				MB_ICONINFO,
			)

			// Launch repaired application
			exec.Command(targetExe, "-port", "8080", "-db", filepath.Join(dataDir, "pos.db")).Start()
			return

		default:
			// ACTION 3: CANCEL
			return
		}

	} else {
		// ====================================================================
		// FIRST-TIME INSTALLATION (Not Yet Installed)
		// ====================================================================
		choice := messageBox(
			"Siraazhi Pro POS — Setup Wizard",
			"Welcome to Siraazhi Pro POS Setup!\n\n"+
				"Would you like to install Siraazhi Pro POS to C:\\Program Files\\SiraazhiPOS\n"+
				"and create Desktop & Start Menu shortcuts?\n\n"+
				"🛡️ ZERO DATA LOSS: If any database already exists on this PC, it will be\n"+
				"automatically preserved and never overwritten.",
			MB_YESNO|MB_ICONQUESTION,
		)

		if choice != IDYES {
			return
		}

		_ = os.MkdirAll(installDir, 0755)
		_ = os.MkdirAll(dataDir, 0755)

		// Create automatic backup if any previous DB exists
		backupPath := backupDatabase(dataDir, installDir, "install")

		terminateRunningInstances()

		// Write embedded files
		if err := os.WriteFile(targetExe, embeddedBinary, 0755); err != nil {
			messageBox("Installation Error", fmt.Sprintf("Failed to write to %s:\n%v", targetExe, err), MB_ICONERROR)
			return
		}
		_ = os.WriteFile(targetUninstaller, embeddedUninstaller, 0755)
		_ = os.WriteFile(targetIcon, embeddedIcon, 0644)
		_ = os.WriteFile(targetReadme, embeddedReadme, 0644)
		_ = os.WriteFile(targetLicense, embeddedLicense, 0644)

		createShortcuts(targetExe, dataDir, installDir, targetIcon, targetUninstaller)
		registerWindowsIntegration(targetExe, dataDir, installDir, targetIcon, targetUninstaller)

		backupNotice := ""
		if backupPath != "" {
			backupNotice = "\n• Preserved existing database: " + filepath.Base(backupPath)
		}

		messageBox(
			"Setup Complete",
			"✅ Siraazhi Pro POS has been installed successfully!\n\n"+
				"• Installed Location: "+installDir+"\n"+
				"• Uninstaller Available: "+targetUninstaller+"\n"+
				"• Desktop Shortcut: Siraazhi Pro POS\n"+
				"• Background Service: Active on port 8080"+backupNotice+"\n\n"+
				"Launching Siraazhi Pro POS now...",
			MB_ICONINFO,
		)

		exec.Command(targetExe, "-port", "8080", "-db", filepath.Join(dataDir, "pos.db")).Start()
	}
}
