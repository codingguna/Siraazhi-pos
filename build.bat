@echo off
setlocal
REM ============================================================================
REM Siraazhi Pro POS -- 1-Click Automated Build and Installer Script
REM Modeled after BioVault Setup Build Pipeline
REM ============================================================================

title Building Siraazhi Pro POS and Inno Setup Installer
cd /d "%~dp0"

echo.
echo ==================================================================
echo       Building Siraazhi Pro POS Components
echo ==================================================================
echo.

REM [1] Compile Main Standalone POS
echo [*] Step 1: Compiling Main Standalone POS (siraazhi-pos.exe)...
go build -v -ldflags "-s -w -H=windowsgui" -o siraazhi-pos.exe .
if errorlevel 1 (
    echo.
    echo [-] Error: Failed to compile main Go binary.
    pause
    exit /b 1
)
echo [+] Successfully generated: siraazhi-pos.exe

REM [2] Locate Inno Setup Compiler ISCC.exe
echo.
echo [*] Step 2: Detecting Inno Setup Compiler (ISCC.exe)...

set "ISCC_EXE="
where iscc >nul 2>&1
if not errorlevel 1 (
    for /f "delims=" %%i in ('where iscc') do set "ISCC_EXE=%%i"
)

if not defined ISCC_EXE if exist "C:\Program Files\Inno Setup 7\ISCC.exe" set "ISCC_EXE=C:\Program Files\Inno Setup 7\ISCC.exe"
if not defined ISCC_EXE if exist "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" set "ISCC_EXE=C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
if not defined ISCC_EXE if exist "C:\Program Files\Inno Setup 6\ISCC.exe" set "ISCC_EXE=C:\Program Files\Inno Setup 6\ISCC.exe"
if not defined ISCC_EXE if exist "%LOCALAPPDATA%\Programs\Inno Setup 7\ISCC.exe" set "ISCC_EXE=%LOCALAPPDATA%\Programs\Inno Setup 7\ISCC.exe"
if not defined ISCC_EXE if exist "%LOCALAPPDATA%\Programs\Inno Setup 6\ISCC.exe" set "ISCC_EXE=%LOCALAPPDATA%\Programs\Inno Setup 6\ISCC.exe"

if not defined ISCC_EXE (
    echo.
    echo [-] Error: Inno Setup compiler ISCC.exe not found on this system!
    echo To compile the Windows installer, install Inno Setup:
    echo   Run: winget install JRSoftware.InnoSetup.7
    echo   Or download from: https://jrsoftware.org/isdl.php
    echo.
    echo Portable application siraazhi-pos.exe was built successfully.
    pause
    exit /b 0
)

echo [+] Using Inno Setup Compiler: "%ISCC_EXE%"

REM [3] Ensure output directory exists
if not exist "output" mkdir "output"
if exist "SiraazhiPOS_Setup.exe" del /f /q "SiraazhiPOS_Setup.exe" >nul 2>&1

REM [4] Compile Inno Setup Script
echo.
echo [*] Step 3: Compiling BioVault-Style Windows Installer (installer.iss)...
"%ISCC_EXE%" installer.iss
if errorlevel 1 (
    echo.
    echo [-] Error: Inno Setup compilation failed!
    pause
    exit /b 1
)

echo.
echo ==================================================================
echo [+] SUCCESS! Final Output Files Generated:
echo ==================================================================
echo.
dir "output\*.exe"
echo.
echo Standalone Portable: siraazhi-pos.exe
echo ==================================================================
echo.
pause
