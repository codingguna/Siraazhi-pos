@echo off
setlocal
REM ============================================================================
REM Siraazhi Pro POS -- 1-Click Automated Build and Installer Script (Inno Setup)
REM WHAT IT DOES:
REM   1. Compiles the main standalone app (siraazhi-pos.exe).
REM   2. Uses -H=windowsgui so NO black command prompt window ever appears!
REM   3. Locates Inno Setup Compiler (ISCC.exe) in PATH or standard Program Files.
REM   4. Compiles installer.iss into a single, high-compression Windows installer
REM      (SiraazhiPOS_Setup.exe) with embedded app icon, autostart, and uninstaller.
REM ============================================================================

title Building Siraazhi Pro POS and Inno Setup Installer
cd /d "%~dp0"

echo.
echo ==================================================================
echo   Building Siraazhi Pro POS Application and Inno Setup Installer
echo ==================================================================
echo.

REM [1] Compile Main Standalone POS
echo [1/3] Compiling Main Standalone POS: siraazhi-pos.exe...
go build -v -ldflags "-s -w -H=windowsgui" -o siraazhi-pos.exe .
if errorlevel 1 (
    echo.
    echo [ERROR] Failed to compile main Go binary: siraazhi-pos.exe
    pause
    exit /b 1
)
echo       --^> Successfully generated: siraazhi-pos.exe

REM [2] Locate Inno Setup Compiler ISCC.exe
echo.
echo [2/3] Detecting Inno Setup Compiler: ISCC.exe...

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
    echo [WARNING] Inno Setup compiler ISCC.exe not found on this system!
    echo To compile the Windows installer, install Inno Setup:
    echo   Run: winget install JRSoftware.InnoSetup.7
    echo   Or download from: https://jrsoftware.org/isdl.php
    echo.
    echo The portable application siraazhi-pos.exe was built successfully.
    pause
    exit /b 0
)

echo       --^> Found Inno Setup: "%ISCC_EXE%"

REM [3] Compile Inno Setup Script
echo.
echo [3/3] Compiling Professional Windows Installer: installer.iss...
"%ISCC_EXE%" installer.iss
if errorlevel 1 (
    echo.
    echo [ERROR] Inno Setup compilation failed!
    pause
    exit /b 1
)

echo.
echo ==================================================================
echo   BUILD COMPLETED SUCCESSFULLY!
echo ==================================================================
echo.
echo   Final Output Files:
echo.
echo   1. SiraazhiPOS_Setup.exe  --^> Single Windows Setup wizard
echo                                 Includes official icon, autostart, and uninstaller
echo.
echo   2. siraazhi-pos.exe       --^> Standalone portable binary
echo ==================================================================
echo.
pause
