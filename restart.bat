@echo off
rem Full restart: stop -> clean stale server tunnel sessions -> rebuild -> start -> verify
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0restart-gateway.ps1" %*
