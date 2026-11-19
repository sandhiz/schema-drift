@echo off
cd /d "%~dp0"
if exist "schema-drift.exe" (
    schema-drift.exe serve --verbose %*
) else (
    go run . serve --verbose %*
)
if errorlevel 1 pause
