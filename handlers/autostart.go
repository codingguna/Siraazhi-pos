/* ==========================================================================
   Package handlers — Windows Auto-Startup Controller & Registry Sync

   WHAT IT IS:
     Enables and manages automatic startup of Siraazhi Pro POS when Windows
     boots or logs in, configurable directly from the POS Settings page.

   HOW IT WORKS:
     1. Uses HKCU\Software\Microsoft\Windows\CurrentVersion\Run for reliable,
        non-elevated user startup without UAC popups.
     2. Supports two startup modes:
        - "window"   -> Launches the standalone POS application window on desktop.
        - "headless" -> Runs quietly in the background on port 8080 for mobile/tablets.
     3. Synchronizes with both pos.db settings and the Windows Registry.
   ========================================================================== */

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"siraazhi-pos/db"
	"strings"
)

// AutoStartResponse contains the current status of Windows Auto-Startup.
type AutoStartResponse struct {
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`
	Command     string `json:"command"`
	ExePath     string `json:"exePath"`
	IsInstalled bool   `json:"isInstalled"`
	StatusText  string `json:"statusText"`
}

// AutoStartRequest is the payload to enable/disable or change startup mode.
type AutoStartRequest struct {
	Enabled *bool   `json:"enabled"`
	Mode    *string `json:"mode"`
}

// resolveExecutablePath determines the canonical executable path.
// Prioritizes C:\Program Files\SiraazhiPOS\siraazhi-pos.exe if installed.
func resolveExecutablePath() (string, bool) {
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	installedExe := filepath.Join(programFiles, "SiraazhiPOS", "siraazhi-pos.exe")
	if fi, err := os.Stat(installedExe); err == nil && fi.Size() > 0 {
		return installedExe, true
	}

	curExe, err := os.Executable()
	if err == nil {
		return curExe, false
	}
	return "siraazhi-pos.exe", false
}

// resolveDatabasePath locates pos.db in AppData or ProgramData.
func resolveDatabasePath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		userProfile := os.Getenv("USERPROFILE")
		if userProfile != "" {
			appData = filepath.Join(userProfile, "AppData", "Roaming")
		}
	}
	if appData != "" {
		dbPath := filepath.Join(appData, "SiraazhiPOS", "data", "pos.db")
		if fi, err := os.Stat(dbPath); err == nil && fi.Size() > 0 {
			return dbPath
		}
	}

	programData := os.Getenv("ProgramData")
	if programData != "" {
		pDataDb := filepath.Join(programData, "SiraazhiPOS", "data", "pos.db")
		if fi, err := os.Stat(pDataDb); err == nil && fi.Size() > 0 {
			return pDataDb
		}
	}

	return ""
}

// QueryAutoStart checks if SiraazhiPOS is registered in HKCU Run key.
func QueryAutoStart() (bool, string, string) {
	if runtime.GOOS != "windows" {
		return false, "window", ""
	}

	// Check HKCU Run key (authoritative for current user and Task Manager)
	out, err := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS").CombinedOutput()
	if err == nil && len(out) > 0 {
		cmdStr := parseRegValue(string(out))
		mode := "window"
		if strings.Contains(cmdStr, "-headless") {
			mode = "headless"
		}
		return true, mode, cmdStr
	}

	return false, "window", ""
}

// parseRegValue extracts the value data from 'reg query' output.
func parseRegValue(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SiraazhiPOS") {
			parts := strings.SplitN(line, "REG_SZ", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
			partsExpand := strings.SplitN(line, "REG_EXPAND_SZ", 2)
			if len(partsExpand) == 2 {
				return strings.TrimSpace(partsExpand[1])
			}
		}
	}
	return ""
}

// SetAutoStart configures or deletes the Windows Run registry key.
func SetAutoStart(enabled bool, mode string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("Windows auto-startup is only supported on Windows operating systems")
	}

	// Always remove any legacy HKLM entry so Windows Task Manager never shows duplicate entries
	exec.Command("reg", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS", "/f").Run()

	if !enabled {
		// Remove from HKCU
		exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS", "/f").Run()
		return nil
	}

	exePath, _ := resolveExecutablePath()
	dbPath := resolveDatabasePath()

	var cmdStr string
	if mode == "headless" {
		if dbPath != "" {
			cmdStr = fmt.Sprintf(`"%s" -headless -port 8080 -db "%s"`, exePath, dbPath)
		} else {
			cmdStr = fmt.Sprintf(`"%s" -headless -port 8080`, exePath)
		}
	} else {
		// "window" mode
		if dbPath != "" {
			cmdStr = fmt.Sprintf(`"%s" -port 8080 -db "%s"`, exePath, dbPath)
		} else {
			cmdStr = fmt.Sprintf(`"%s" -port 8080`, exePath)
		}
	}

	// Register in HKCU\Software\Microsoft\Windows\CurrentVersion\Run
	cmd := exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "SiraazhiPOS", "/t", "REG_SZ", "/d", cmdStr, "/f")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to register startup in registry: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// HandleAutoStart handles GET and POST/PUT for /api/autostart.
func HandleAutoStart(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		enabled, mode, cmdStr := QueryAutoStart()
		exePath, isInstalled := resolveExecutablePath()
		statusText := "Disabled"
		if enabled {
			if mode == "headless" {
				statusText = "Active (Silent Background on Port 8080)"
			} else {
				statusText = "Active (Billing Window Opens on Boot)"
			}
		}

		JSON(w, http.StatusOK, AutoStartResponse{
			Enabled:     enabled,
			Mode:        mode,
			Command:     cmdStr,
			ExePath:     exePath,
			IsInstalled: isInstalled,
			StatusText:  statusText,
		})

	case http.MethodPost, http.MethodPut:
		if !HasMasterAuth(r) {
			ErrorJSON(w, http.StatusUnauthorized, "Master Admin PIN or Host access required")
			return
		}

		var req AutoStartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			ErrorJSON(w, http.StatusBadRequest, "Invalid request payload")
			return
		}

		curEnabled, curMode, _ := QueryAutoStart()
		newEnabled := curEnabled
		if req.Enabled != nil {
			newEnabled = *req.Enabled
		}
		newMode := "headless"
		if req.Mode != nil && *req.Mode != "" {
			newMode = *req.Mode
		} else if curMode != "" {
			newMode = curMode
		}

		if err := SetAutoStart(newEnabled, newMode); err != nil {
			ErrorJSON(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Synchronize state in pos.db settings table
		s, err := db.GetSettings()
		if err == nil {
			s.AutoStartup = newEnabled
			s.AutoStartupMode = newMode
			_ = db.SaveSettings(s)
		}

		statusText := "Disabled"
		if newEnabled {
			if newMode == "headless" {
				statusText = "Active (Silent Background on Port 8080)"
			} else {
				statusText = "Active (Billing Window Opens on Boot)"
			}
		}

		exePath, isInstalled := resolveExecutablePath()
		_, _, cmdStr := QueryAutoStart()

		JSON(w, http.StatusOK, AutoStartResponse{
			Enabled:     newEnabled,
			Mode:        newMode,
			Command:     cmdStr,
			ExePath:     exePath,
			IsInstalled: isInstalled,
			StatusText:  statusText,
		})

	default:
		ErrorJSON(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
