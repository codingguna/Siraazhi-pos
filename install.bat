@echo off
:: ============================================================================
:: Siraazhi Pro POS — Official Windows Installation Script
:: WHAT IT DOES:
::   1. Requests Administrator privileges.
::   2. Installs executable to C:\Program Files\SiraazhiPOS\
::   3. Sets database directory to C:\ProgramData\SiraazhiPOS\data\
::   4. Creates Desktop & Start Menu shortcuts.
::   5. Registers silent auto-start with Windows boot.
::   6. Registers in Windows Settings / Control Panel (Add or Remove Programs).
:: ============================================================================

title Installing Siraazhi Pro POS...
cd /d "%~dp0"

:: Check for Administrator Rights
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo ================================================================
    echo  ADMINISTRATOR PERMISSION REQUIRED
    echo ================================================================
    echo  Please right-click on "install.bat" and select "Run as administrator".
    echo ================================================================
    pause
    exit /b 1
)

echo.
echo ================================================================
echo   Installing Siraazhi Pro POS to C:\Program Files\SiraazhiPOS
echo ================================================================
echo.

set "INSTALL_DIR=%ProgramFiles%\SiraazhiPOS"
set "DATA_DIR=%ProgramData%\SiraazhiPOS\data"
set "BACKUP_DIR=%ProgramData%\SiraazhiPOS\backups"

if not exist "%BACKUP_DIR%" mkdir "%BACKUP_DIR%" >nul 2>&1

:: Check if already installed
if exist "%INSTALL_DIR%\siraazhi-pos.exe" (
    echo ================================================================
    echo  EXISTING INSTALLATION DETECTED IN:
    echo  %INSTALL_DIR%
    echo ================================================================
    echo  Please choose an action:
    echo.
    echo   [1] MODIFY / UPDATE  - Update old files with new updated version
    echo   [2] REPAIR           - Scan files and fix any broken/missing files
    echo   [3] CANCEL           - Exit setup without making changes
    echo.
    echo  Note: Your database and sales data will be safely preserved.
    echo ================================================================
    set /p "MAINT_CHOICE=Enter your choice [1, 2, or 3]: "
    if "%MAINT_CHOICE%"=="3" (
        echo Setup cancelled by user.
        exit /b 0
    )
    if "%MAINT_CHOICE%"=="2" (
        echo.
        echo Starting Repair Mode...
    ) else (
        echo.
        echo Starting Modify / Update Mode...
    )
)

:: 1. Create Directories & Safety Backup
echo [1/6] Creating Program Directories & Safety Backup...
if not exist "%INSTALL_DIR%" mkdir "%INSTALL_DIR%"
if not exist "%DATA_DIR%" mkdir "%DATA_DIR%"

:: Automatic timestamped DB safety backup
if exist "%DATA_DIR%\pos.db" (
    for /f "usebackq delims=" %%T in (`powershell -NoProfile -Command "Get-Date -Format 'yyyyMMdd_HHmmss'"`) do (
        copy /y "%DATA_DIR%\pos.db" "%BACKUP_DIR%\pos_backup_%%T.db" >nul 2>&1
    )
)

:: 2. Copy Executable & Assets
echo [2/6] Copying Software Files...
taskkill /f /im siraazhi-pos.exe >nul 2>&1
copy /y "siraazhi-pos.exe" "%INSTALL_DIR%\siraazhi-pos.exe" >nul
copy /y "app.ico" "%INSTALL_DIR%\app.ico" >nul 2>&1
copy /y "start_silent.vbs" "%INSTALL_DIR%\start_silent.vbs" >nul
copy /y "README.md" "%INSTALL_DIR%\README.md" >nul

:: Copy initial DB ONLY IF none exists in ProgramData
if not exist "%DATA_DIR%\pos.db" (
    if exist "data\pos.db" (
        copy /y "data\pos.db" "%DATA_DIR%\pos.db" >nul
    )
)

:: 3. Create Windows Shortcuts (Desktop & Start Menu) using PowerShell
echo [3/6] Creating Desktop & Start Menu Shortcuts...
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ws = New-Object -ComObject WScript.Shell; " ^
  "$desk = [Environment]::GetFolderPath('Desktop'); " ^
  "$s1 = $ws.CreateShortcut(\"$desk\Siraazhi Pro POS.lnk\"); " ^
  "$s1.TargetPath = '%INSTALL_DIR%\siraazhi-pos.exe'; " ^
  "$s1.Arguments = '-port 8080 -db \"\"%DATA_DIR%\pos.db\"\"'; " ^
  "$s1.WorkingDirectory = '%INSTALL_DIR%'; " ^
  "$s1.IconLocation = '%INSTALL_DIR%\siraazhi-pos.exe,0'; " ^
  "$s1.Description = 'Siraazhi Pro POS — Billing & Inventory Suite'; " ^
  "$s1.Save(); " ^
  "$startMenu = [Environment]::GetFolderPath('CommonStartMenu'); " ^
  "$s2 = $ws.CreateShortcut(\"$startMenu\Programs\Siraazhi Pro POS.lnk\"); " ^
  "$s2.TargetPath = '%INSTALL_DIR%\siraazhi-pos.exe'; " ^
  "$s2.Arguments = '-port 8080 -db \"\"%DATA_DIR%\pos.db\"\"'; " ^
  "$s2.WorkingDirectory = '%INSTALL_DIR%'; " ^
  "$s2.IconLocation = '%INSTALL_DIR%\siraazhi-pos.exe,0'; " ^
  "$s2.Save();"

:: 4. Register Auto-Start with Windows Boot (Runs Silently in Background)
echo [4/6] Configuring Background Auto-Start on Windows Boot...
reg delete "HKLM\Software\Microsoft\Windows\CurrentVersion\Run" /v "SiraazhiPOS" /f >nul 2>&1
reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "SiraazhiPOS" /t REG_SZ /d "\"%INSTALL_DIR%\siraazhi-pos.exe\" -headless -port 8080 -db \"%DATA_DIR%\pos.db\"" /f >nul

:: 5. Register in Windows "Installed Apps" / Control Panel Add-Remove Programs
echo [5/6] Registering in Windows Add/Remove Programs...
set "REG_UNINSTALL=HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\SiraazhiPOS"
reg add "%REG_UNINSTALL%" /v "DisplayName" /t REG_SZ /d "Siraazhi Pro POS (Vanur FPC Ltd)" /f >nul
reg add "%REG_UNINSTALL%" /v "DisplayVersion" /t REG_SZ /d "1.0.0" /f >nul
reg add "%REG_UNINSTALL%" /v "Publisher" /t REG_SZ /d "Vanur Farmer Producer Company Limited" /f >nul
reg add "%REG_UNINSTALL%" /v "InstallLocation" /t REG_SZ /d "%INSTALL_DIR%" /f >nul
reg add "%REG_UNINSTALL%" /v "UninstallString" /t REG_SZ /d "\"%INSTALL_DIR%\uninstall.bat\"" /f >nul

:: Copy uninstaller
copy /y "uninstall.bat" "%INSTALL_DIR%\uninstall.bat" >nul 2>&1

:: 6. Complete
echo [6/6] Installation Complete!
echo.
echo ================================================================
echo   SUCCESS! Siraazhi Pro POS is installed as Windows Software.
echo   - Installed in: %INSTALL_DIR%
echo   - Database in:  %DATA_DIR%
echo   - Desktop icon: "Siraazhi Pro POS"
echo   - Auto-starts:  Silently in the background on port 8080
echo ================================================================
echo.
pause
