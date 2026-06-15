@echo off
tasklist /fi "imagename eq serial-gateway.exe" | findstr /i "serial-gateway.exe" >nul
if %errorlevel%==0 (
    echo serial-gateway.exe is RUNNING:
    tasklist /fi "imagename eq serial-gateway.exe"
) else (
    echo serial-gateway.exe is NOT running.
)
