@echo off
title Bounty Chat Console

echo ============================================
echo   Bounty Chat Console
echo   http://localhost:8090
echo ============================================
echo.

echo Starting Bounty server...
start "" http://localhost:8090
bounty.exe dashboard
