@echo off
setlocal
"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1"
set "BIRDCAGE_EXIT=%ERRORLEVEL%"
echo.
if not "%BIRDCAGE_EXIT%"=="0" echo Installation failed with exit code %BIRDCAGE_EXIT%.
pause
exit /b %BIRDCAGE_EXIT%
