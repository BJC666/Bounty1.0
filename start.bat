@echo off
title Bounty 1.0 - AI Agent x DeVET

echo ============================================
echo   Bounty 1.0 - AI Agent x DeVET
echo ============================================
echo.

REM Load env vars from registry (set via setx in previous sessions)
for /f "tokens=2* delims= " %%a in ('reg query HKCU\Environment /v DEEPSEEK_API_KEY 2^>nul ^| find "DEEPSEEK_API_KEY"') do set "DEEPSEEK_API_KEY=%%b"
for /f "tokens=2* delims= " %%a in ('reg query HKCU\Environment /v ANTHROPIC_API_KEY 2^>nul ^| find "ANTHROPIC_API_KEY"') do set "ANTHROPIC_API_KEY=%%b"

if "%DEEPSEEK_API_KEY%"=="" (
    echo [WARNING] DEEPSEEK_API_KEY not set!
    echo   Run: setx DEEPSEEK_API_KEY "sk-your-key"
    echo   Then re-open this window.
    echo.
    pause
    exit /b 1
)

echo [1/2] Starting DeVET backend...
start "DeVET" cmd /c "cd /d %~dp0..\DeVET\backend && python server.py"
echo   Waiting for DeVET backend (3s)...
timeout /t 3 >nul

echo [2/2] Starting Bounty Agent...
echo.
bounty.exe chat

echo.
echo Bounty 1.0 session ended.
pause
