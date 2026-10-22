@echo off
:: ============================================================================
:: Siraazhi Pro POS — 1-Click Automated Build Script with Icon Embedding
:: WHAT IT DOES:
::   1. Generates Windows resource .syso files from app.ico.
::   2. Compiles the main standalone app (siraazhi-pos.exe) with embedded icon & assets.
::   3. Staged binary & icon into installer package.
::   4. Compiles the self-extracting installer (SiraazhiPOS-Setup.exe) with embedded icon.
::   5. Uses -H=windowsgui so NO black command prompt window ever appears!
:: ============================================================================

title Building Siraazhi Pro POS
cd /d "%~dp0"

echo.
echo ==================================================================
echo   Building Siraazhi Pro POS Final Output Executables (with Icon)
echo ==================================================================
echo.

:: 1. Generate Windows Icon Resources
echo [1/5] Embedding Windows App Icon (app.ico)...
if exist "app.ico" (
    rsrc -ico app.ico -o rsrc.syso >nul 2>&1
    if not exist "installer" mkdir "installer"
    copy /y "app.ico" "installer\app.ico" >nul 2>&1
    rsrc -ico installer\app.ico -o installer\rsrc.syso >nul 2>&1
    if not exist "uninstaller" mkdir "uninstaller"
    rsrc -ico app.ico -o uninstaller\rsrc.syso >nul 2>&1
)

:: 2. Compile Main Application
echo [2/5] Compiling Main Standalone POS (siraazhi-pos.exe)...
go build -v -ldflags "-s -w -H=windowsgui" -o siraazhi-pos.exe .
if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Failed to compile main binary.
    pause
    exit /b %errorlevel%
)
echo       -^> Generated: siraazhi-pos.exe (with Official Logo Icon)

:: 3. Compile Standalone GUI Uninstaller
echo [3/5] Compiling Standalone Uninstaller (uninstall.exe)...
go build -v -ldflags "-s -w -H=windowsgui" -o uninstall.exe .\uninstaller
if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Failed to compile uninstaller.
    pause
    exit /b %errorlevel%
)
echo       -^> Generated: uninstall.exe (with Official Logo Icon)

:: 4. Stage components for installer embedding
echo [4/5] Staging components into Setup Installer package...
copy /y "siraazhi-pos.exe" "installer\siraazhi-pos.exe" >nul
copy /y "uninstall.exe" "installer\uninstall.exe" >nul
copy /y "app.ico" "installer\app.ico" >nul
copy /y "README.txt" "installer\README.txt" >nul
copy /y "LICENSE.txt" "installer\LICENSE.txt" >nul

:: 5. Compile Standalone Installer
echo [5/5] Compiling Standalone Windows Setup (SiraazhiPOS-Setup.exe)...
go build -tags installer -v -ldflags "-s -w -H=windowsgui" -o SiraazhiPOS-Setup.exe .\installer
if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Failed to compile installer.
    pause
    exit /b %errorlevel%
)
echo       -^> Generated: SiraazhiPOS-Setup.exe (with Official Setup Icon)

:: Clean up staged temporary files
del /f /q "installer\siraazhi-pos.exe" >nul 2>&1
del /f /q "installer\uninstall.exe" >nul 2>&1
del /f /q "installer\README.txt" >nul 2>&1
del /f /q "installer\LICENSE.txt" >nul 2>&1

echo.
echo ==================================================================
echo   BUILD COMPLETED SUCCESSFULLY!
echo ==================================================================
echo.
echo   Final Output Files in this folder:
echo.
echo   1. SiraazhiPOS-Setup.exe  -^> The single installer to upload to GitHub
echo                               (with Windows Logo Icon)
echo.
echo   2. siraazhi-pos.exe       -^> The portable standalone application
echo                               (with Windows Logo Icon)
echo.
echo   3. uninstall.exe          -^> Standalone GUI uninstaller
echo ==================================================================
echo.
pause
