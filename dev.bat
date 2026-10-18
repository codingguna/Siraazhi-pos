@echo off
:: ============================================================================
:: Siraazhi Pro POS — 1-Click Development & Testing Runner (No Install Needed)
:: ============================================================================

title Siraazhi Pro POS — Dev Server
cd /d "%~dp0"

echo.
echo ==================================================================
echo   Siraazhi Pro POS — Live Development Mode (No Install Needed)
echo ==================================================================
echo.

:: 1. Stop any currently active POS instance to free port 8080
taskkill /f /im siraazhi-pos.exe >nul 2>&1

:: 2. Ensure local data directory exists
if not exist "data" mkdir "data"

echo   [1/2] Local Database: %cd%\data\pos.db
echo   [2/2] Launching Siraazhi POS on port 8080...
echo.
echo   • Master Terminal: http://localhost:8080
echo   • Phone / Tablet:  http://10.98.92.48:8080  (on same Wi-Fi)
echo.
echo   Press Ctrl+C in this window anytime to stop the dev server.
echo ==================================================================
echo.

:: 3. Run the compiled standalone executable with local database
if exist "siraazhi-pos.exe" (
    siraazhi-pos.exe -port 8080 -db "data\pos.db"
) else (
    go run . -port 8080 -db "data\pos.db"
)

pause
