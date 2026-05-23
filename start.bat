@echo off
@REM 使用GBK编码避免中文乱码问题
chcp 936 >nul
title ChatLog Launcher

echo Starting Go application...
start "ChatLog Go" cmd /k "cd /d "%~dp0" && go run main.go"

timeout /t 3 /nobreak >nul

echo Starting Web application...
start "ChatLog Web" cmd /k "cd /d "%~dp0\chatmodel" && E:\\miniconda3\\envs\\funasr\\python.exe web_app.py"

echo Waiting for web application to start...
timeout /t 5 /nobreak >nul

echo Opening browser to http://localhost:8000...
start "" "http://localhost:8000"

echo.
echo Both applications have been started!
echo 1. Go application is running
echo 2. Web application is running on http://localhost:8000
echo 3. Browser has been opened to the web interface
echo.
echo Press any key to close this window...
pause >nul