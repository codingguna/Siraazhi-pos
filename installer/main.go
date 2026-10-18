/* ==========================================================================
   Siraazhi Pro POS — Official Self-Extracting Windows Setup Installer
   
   WHAT IT IS:
     A single standalone Windows installer (.exe) that users download from GitHub:
     - Embeds the complete 10.8 MB Siraazhi POS binary inside itself.
     - Automatically installs the software into C:\Program Files\SiraazhiPOS\
     - Creates the Desktop shortcut ("Siraazhi Pro POS")
     - Creates the Start Menu shortcut
     - Registers silent Windows background startup on boot
     - Registers in Windows Control Panel / Settings (Add or Remove Programs)
     - Automatically launches the POS native desktop window upon completion!

   HOW IT WORKS:
     1. Uses //go:embed to pack siraazhi-pos.exe directly into SiraazhiPOS-Setup.exe.
     2. Elevates to Administrator privileges via Windows UAC if needed.
     3. Prompts user with a native Windows confirmation dialog.
     4. Writes the binary to C:\Program Files\SiraazhiPOS\siraazhi-pos.exe.
     5. Creates Windows shell shortcuts using WScript.Shell.
     6. Launches the application immediately.

   FLOW:
     Download SiraazhiPOS-Setup.exe -> Double Click -> One-Click Install -> Desktop Shortcut Ready
   ========================================================================== */

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
	user32           = syscall.NewLazyDLL("user32.dll")
	shell32          = syscall.NewLazyDLL("shell32.dll")
	procMessageBoxW  = user32.NewProc("MessageBoxW")
	procShellExecute = shell32.NewProc("ShellExecuteW")
)

const (
	MB_YESNO       = 0x00000004
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
		// Request UAC elevation
		runAsAdmin()
		return
	}

	choice := messageBox(
		"Siraazhi Pro POS — Setup Wizard",
		"Welcome to Siraazhi Pro POS Setup!\n\n"+
			"Would you like to install Siraazhi Pro POS to C:\\Program Files\\SiraazhiPOS\n"+
			"and create Desktop & Start Menu shortcuts?",
		MB_YESNO|MB_ICONQUESTION,
	)

	if choice != IDYES {
		return
	}

	// 1. Target Directories
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	installDir := filepath.Join(programFiles, "SiraazhiPOS")
	dataDir := filepath.Join(os.Getenv("ProgramData"), "SiraazhiPOS", "data")

	_ = os.MkdirAll(installDir, 0755)
	_ = os.MkdirAll(dataDir, 0755)

	targetExe := filepath.Join(installDir, "siraazhi-pos.exe")
	targetUninstaller := filepath.Join(installDir, "uninstall.exe")
	targetIcon := filepath.Join(installDir, "app.ico")
	targetReadme := filepath.Join(installDir, "README.txt")
	targetLicense := filepath.Join(installDir, "LICENSE.txt")

	// 2. Terminate any running instances before overwriting
	exec.Command("taskkill", "/f", "/im", "siraazhi-pos.exe").Run()

	// 3. Write embedded executable and components
	if err := os.WriteFile(targetExe, embeddedBinary, 0755); err != nil {
		messageBox("Installation Error", fmt.Sprintf("Failed to write to %s:\n%v", targetExe, err), MB_ICONERROR)
		return
	}
	_ = os.WriteFile(targetUninstaller, embeddedUninstaller, 0755)
	_ = os.WriteFile(targetIcon, embeddedIcon, 0644)
	_ = os.WriteFile(targetReadme, embeddedReadme, 0644)
	_ = os.WriteFile(targetLicense, embeddedLicense, 0644)

	// 4. Create Desktop & Start Menu Shortcuts using PowerShell
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

	// 5. Register auto-start in Windows Run registry
	regCmd := fmt.Sprintf(`reg add "HKLM\Software\Microsoft\Windows\CurrentVersion\Run" /v "SiraazhiPOS" /t REG_SZ /d "\"%s\" -headless -port 8080 -db \"%s\pos.db\"" /f`, targetExe, dataDir)
	exec.Command("cmd", "/c", regCmd).Run()

	// 6. Register in Windows Settings / Control Panel (Add or Remove Programs)
	regUninstall := `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\SiraazhiPOS`
	exec.Command("reg", "add", regUninstall, "/v", "DisplayName", "/t", "REG_SZ", "/d", "Siraazhi Pro POS (Vanur FPC Ltd)", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", "1.0.0", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "Publisher", "/t", "REG_SZ", "/d", "Vanur Farmer Producer Company Limited", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "InstallLocation", "/t", "REG_SZ", "/d", installDir, "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "DisplayIcon", "/t", "REG_SZ", "/d", targetIcon, "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "UninstallString", "/t", "REG_SZ", "/d", fmt.Sprintf("\"%s\"", targetUninstaller), "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "QuietUninstallString", "/t", "REG_SZ", "/d", fmt.Sprintf("\"%s\"", targetUninstaller), "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "EstimatedSize", "/t", "REG_DWORD", "/d", "32000", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1", "/f").Run()
	exec.Command("reg", "add", regUninstall, "/v", "NoRepair", "/t", "REG_DWORD", "/d", "1", "/f").Run()

	// 7. Show success confirmation
	messageBox(
		"Setup Complete",
		"✅ Siraazhi Pro POS has been installed successfully!\n\n"+
			"• Installed Location: "+installDir+"\n"+
			"• Uninstaller Available: "+targetUninstaller+"\n"+
			"• Desktop Shortcut: Siraazhi Pro POS\n"+
			"• Background Service: Active on port 8080\n\n"+
			"Launching Siraazhi Pro POS now...",
		MB_ICONINFO,
	)

	// 8. Launch the installed app in Native Desktop Window
	exec.Command(targetExe, "-port", "8080", "-db", filepath.Join(dataDir, "pos.db")).Start()
}
