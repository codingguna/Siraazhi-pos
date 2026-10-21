@echo off
:: Siraazhi Pro POS — Uninstallation Script
title Uninstalling Siraazhi Pro POS...

net session >nul 2>&1
if %errorlevel% neq 0 (
    echo Administrator permission required. Right click and Run as administrator.
    pause
    exit /b 1
)

echo Terminating running POS processes...
taskkill /f /im "siraazhi-pos.exe" >nul 2>&1

echo Removing Desktop and Start Menu Shortcuts...
powershell -NoProfile -Command ^
  "Remove-Item -Path \"$([Environment]::GetFolderPath('Desktop'))\Siraazhi Pro POS.lnk\" -Force -ErrorAction SilentlyContinue; " ^
  "Remove-Item -Path \"$([Environment]::GetFolderPath('CommonStartMenu'))\Programs\Siraazhi Pro POS.lnk\" -Force -ErrorAction SilentlyContinue;"

echo Removing Windows Boot Auto-Start...
reg delete "HKLM\Software\Microsoft\Windows\CurrentVersion\Run" /v "SiraazhiPOS" /f >nul 2>&1

echo Removing Control Panel Registration...
reg delete "HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\SiraazhiPOS" /f >nul 2>&1

echo Safeguarding database and creating backup...
if not exist "%ProgramData%\SiraazhiPOS\data" mkdir "%ProgramData%\SiraazhiPOS\data" >nul 2>&1
if not exist "%ProgramData%\SiraazhiPOS\backups" mkdir "%ProgramData%\SiraazhiPOS\backups" >nul 2>&1
if exist "%ProgramFiles%\SiraazhiPOS\data\pos.db" (
    if not exist "%ProgramData%\SiraazhiPOS\data\pos.db" (
        copy /y "%ProgramFiles%\SiraazhiPOS\data\pos.db" "%ProgramData%\SiraazhiPOS\data\pos.db" >nul 2>&1
    )
    copy /y "%ProgramFiles%\SiraazhiPOS\data\pos.db" "%ProgramData%\SiraazhiPOS\backups\pos_backup_uninstall.db" >nul 2>&1
)
if exist "%ProgramData%\SiraazhiPOS\data\pos.db" (
    copy /y "%ProgramData%\SiraazhiPOS\data\pos.db" "%ProgramData%\SiraazhiPOS\backups\pos_backup_uninstall.db" >nul 2>&1
)

echo Removing Program Files...
rmdir /s /q "%ProgramFiles%\SiraazhiPOS" >nul 2>&1

echo.
echo ================================================================
echo  Siraazhi Pro POS has been uninstalled successfully.
echo  Note: Your database in C:\ProgramData\SiraazhiPOS was preserved.
echo ================================================================
echo.
pause
