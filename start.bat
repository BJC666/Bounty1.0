@echo off
title Bounty 1.0 - AI Agent x DeVET

echo ============================================
echo   Bounty 1.0 - AI Agent x DeVET
echo ============================================
echo.

:: Start DeVET backend on port 8765
echo [1/2] Starting DeVET backend...
start "DeVET" cmd /c "cd /d D:\智能体开发\全智赛AI+软件\DeVET\backend && python server.py"
echo   Waiting for DeVET backend (3s)...
timeout /t 3 >nul

:: Start Bounty TUI
echo [2/2] Starting Bounty Agent...
echo.
bounty.exe chat

echo.
echo Bounty 1.0 session ended.
pause
