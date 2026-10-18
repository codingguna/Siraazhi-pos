@echo off
:: ============================================================================
:: Siraazhi Pro POS — 1-Click Automated Build Script
:: WHAT IT DOES:
::   1. Compiles the main standalone app (siraazhi-pos.exe) with embedded assets.
::   2. Copies the compiled binary into the installer folder.
::   3. Compiles the self-extracting installer (SiraazhiPOS-Setup.exe).
::   4. Uses -H=windowsgui so NO black command prompt window ever appears!
:: ============================================================================

title "Building Siraazhi Pro POS"
cd /d "%~dp0"

echo.
echo ==================================================================
echo   Building Siraazhi Pro POS Final Output Executables
echo ==================================================================
echo.

:: 1. Compile Main Application
echo [1/3] Compiling Main Standalone POS (siraazhi-pos.exe)...
go build -v -ldflags "-s -w -H=windowsgui" -o siraazhi-pos.exe .
if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Failed to compile main binary.
    pause
    exit /b %errorlevel%
)
echo       -> Generated: siraazhi-pos.exe

:: 2. Stage binary for installer embedding
echo [2/3] Embedding binary into Setup Installer...
if not exist "installer" mkdir "installer"
copy /y "siraazhi-pos.exe" "installer\siraazhi-pos.exe" >nul

:: 3. Compile Standalone Installer
echo [3/3] Compiling Standalone Windows Setup (SiraazhiPOS-Setup.exe)...
go build -v -ldflags "-s -w -H=windowsgui" -o SiraazhiPOS-Setup.exe .\installer
if %errorlevel% neq 0 (
    echo.
    echo [ERROR] Failed to compile installer.
    pause
    exit /b %errorlevel%
)
echo       -> Generated: SiraazhiPOS-Setup.exe

:: Clean up staged file
del /f /q "installer\siraazhi-pos.exe" >nul 2>&1

echo.
echo ==================================================================
echo   BUILD COMPLETED SUCCESSFULLY!
echo ==================================================================
echo.
echo   Final Output Files in this folder:
echo.
echo   1. SiraazhiPOS-Setup.exe  -^> The single installer to upload to GitHub
echo                               (users download this to install on their PC)
echo.
echo   2. siraazhi-pos.exe       -^> The portable standalone application
echo                               (runs directly without installation)
echo ==================================================================
echo.
pause
