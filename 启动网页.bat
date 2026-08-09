@echo off
title Bounty Chat Console

REM Always run from the script's own directory, no matter where it is double-clicked
cd /d "%~dp0"

echo ============================================
echo   Bounty Chat Console (frontend + DeVET backend)
echo   Backend : http://127.0.0.1:8765/api
echo   Frontend: http://localhost:8090
echo ============================================
echo.

REM Close any stale Bounty process (old binary lacks the cmd quote fix;
REM it must be restarted to load the new build)
tasklist /fi "imagename eq bounty.exe" 2>nul | find /i "bounty.exe" >nul
if not errorlevel 1 (
    echo [1/4] Old Bounty is still running - closing it now...
    taskkill /f /im bounty.exe >nul 2>&1
    timeout /t 1 >nul
)

REM Load env vars from registry (set via setx in previous sessions)
for /f "tokens=2* delims= " %%a in ('reg query HKCU\Environment /v DEEPSEEK_API_KEY 2^>nul ^| find "DEEPSEEK_API_KEY"') do set "DEEPSEEK_API_KEY=%%b"
for /f "tokens=2* delims= " %%a in ('reg query HKCU\Environment /v ANTHROPIC_API_KEY 2^>nul ^| find "ANTHROPIC_API_KEY"') do set "ANTHROPIC_API_KEY=%%b"
for /f "tokens=2* delims= " %%a in ('reg query HKCU\Environment /v QWEN_TOKENPLAN_API_KEY 2^>nul ^| find "QWEN_TOKENPLAN_API_KEY"') do set "QWEN_TOKENPLAN_API_KEY=%%b"

if "%DEEPSEEK_API_KEY%"=="" (
    echo [WARN] DEEPSEEK_API_KEY not set! Run: setx DEEPSEEK_API_KEY "sk-your-key"
    echo.
)
if "%QWEN_TOKENPLAN_API_KEY%"=="" (
    echo [WARN] QWEN_TOKENPLAN_API_KEY not set! Default model qwen/qwen3.8-max needs it.
    echo.
)

if not exist "bounty.exe" (
    echo [ERROR] bounty.exe not found. Make sure this script lives in the Bounty1.0 folder.
    echo.
    pause
    exit /b 1
)

REM [2/4] Start DeVET backend (FastAPI :8765) if it is not already running
netstat -ano | findstr /r ":8765.*LISTENING" >nul
if not errorlevel 1 (
    echo [2/4] DeVET backend already running on :8765
) else (
    echo [2/4] Starting DeVET backend...
    start "DeVET Backend" /d "%~dp0..\DeVET\backend" cmd /k "python server.py"
    timeout /t 3 >nul
    netstat -ano | findstr /r ":8765.*LISTENING" >nul
    if not errorlevel 1 (
        echo       DeVET backend is up: http://127.0.0.1:8765/api
    ) else (
        echo [WARN] DeVET backend did not start - check the "DeVET Backend" window.
    )
)

echo [3/4] Starting Bounty server (new build)...
start "" http://localhost:8090
echo [4/4] bounty.exe dashboard
bounty.exe dashboard