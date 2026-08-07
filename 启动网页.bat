@echo off
title Bounty Chat Console

echo ============================================
echo   Bounty Chat Console
echo   http://localhost:8090
echo ============================================
echo.

echo Starting Bounty server...
start "" http://localhost:8090

REM Load env vars from registry (set via setx in previous sessions)
for /f "tokens=2* delims= " %%a in ('reg query HKCU\Environment /v DEEPSEEK_API_KEY 2^>nul ^| find "DEEPSEEK_API_KEY"') do set "DEEPSEEK_API_KEY=%%b"
for /f "tokens=2* delims= " %%a in ('reg query HKCU\Environment /v ANTHROPIC_API_KEY 2^>nul ^| find "ANTHROPIC_API_KEY"') do set "ANTHROPIC_API_KEY=%%b"

if "%DEEPSEEK_API_KEY%"=="" (
    echo [WARNING] DEEPSEEK_API_KEY not set! Run: setx DEEPSEEK_API_KEY "sk-your-key"
    echo.
)

bounty.exe dashboard
