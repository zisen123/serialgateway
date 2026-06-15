@echo off
echo Stopping serial-gateway.exe ...
taskkill /f /im serial-gateway.exe >nul 2>&1
timeout /t 1 /nobreak >nul
echo Starting serial-gateway.exe ...
cscript //nologo "%~dp0run.vbs"
echo serial-gateway.exe restarted.
