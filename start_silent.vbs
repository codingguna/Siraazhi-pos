' Siraazhi Pro POS — Silent Background Launcher (Windows)
' WHAT IT IS:
'   A standard Windows VBScript runner that launches the single executable
'   siraazhi-pos.exe in the background with zero visible console window.
' HOW IT WORKS:
'   Uses WScript.Shell.Run with window style 0 (completely hidden/silent).
'   Listens on http://localhost:8080 and http://<your-lan-ip>:8080 continuously.

Set WshShell = CreateObject("WScript.Shell")
WshShell.Run "siraazhi-pos.exe -port 8080", 0, False
Set WshShell = Nothing
