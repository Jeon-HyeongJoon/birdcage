@echo off
setlocal
"%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File "%~dp0uninstall.ps1"
set "BIRDCAGE_EXIT=%ERRORLEVEL%"
echo.
if not "%BIRDCAGE_EXIT%"=="0" echo Removal failed with exit code %BIRDCAGE_EXIT%.
pause
exit /b %BIRDCAGE_EXIT%
