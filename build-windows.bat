@echo off
setlocal EnableExtensions
cd /d "%~dp0"
if errorlevel 1 (
  echo ERROR: Cannot open project folder: %~dp0
  pause
  exit /b 1
)

set "ROOT=%CD%"
set "LOG=%ROOT%\build-windows.log"
set "PSLOG=%ROOT%\build-ps.log"
set "PS1=%ROOT%\scripts\build-windows.ps1"

echo [%DATE% %TIME%] build-windows.bat started > "%LOG%"
echo CD=%ROOT%>> "%LOG%"
echo BAT=%~f0>> "%LOG%"

echo.
echo === Jorbique VPN Extended Windows build ===
echo Project: %ROOT%
echo Log: %LOG%
echo.

where go >nul 2>&1
if errorlevel 1 (
  echo ERROR: go not in PATH. Install Go, reopen CMD. Do not use Admin CMD.>> "%LOG%"
  echo ERROR: go not in PATH. Install Go, reopen CMD. Do not use Admin CMD.
  goto fail
)

where node >nul 2>&1
if errorlevel 1 (
  echo ERROR: node not in PATH. Install Node.js LTS.>> "%LOG%"
  echo ERROR: node not in PATH. Install Node.js LTS.
  goto fail
)

where npm >nul 2>&1
if errorlevel 1 (
  echo ERROR: npm not in PATH.>> "%LOG%"
  echo ERROR: npm not in PATH.
  goto fail
)

where git >nul 2>&1
if errorlevel 1 (
  echo ERROR: git not in PATH. Install Git for Windows and reopen CMD.>> "%LOG%"
  echo ERROR: git not in PATH. Install Git for Windows and reopen CMD.
  goto fail
)

if not exist "%PS1%" (
  echo ERROR: missing %PS1%>> "%LOG%"
  echo ERROR: missing %PS1%
  goto fail
)

echo --- go version --->> "%LOG%"
call go version >> "%LOG%" 2>&1
call go version

echo --- node -v --->> "%LOG%"
call node -v >> "%LOG%" 2>&1
call node -v

echo --- npm -v --->> "%LOG%"
call npm -v >> "%LOG%" 2>&1
call npm -v

echo.>> "%LOG%"
echo Starting PowerShell build...>> "%LOG%"
echo.
echo Starting PowerShell build...
echo PowerShell log: %PSLOG%
echo.

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%PS1%" %* > "%PSLOG%" 2>&1
set "RC=%ERRORLEVEL%"

echo.>> "%LOG%"
echo === PowerShell log ===>> "%LOG%"
type "%PSLOG%" >> "%LOG%"
echo.>> "%LOG%"
echo PowerShell exit code: %RC%>> "%LOG%"

echo.
echo --- build log ---
type "%LOG%"
echo --- end ---

if not "%RC%"=="0" (
  echo.
  echo BUILD FAILED exit %RC%
  echo See %LOG% and %PSLOG%
  goto fail
)

echo.
echo BUILD OK
echo   %ROOT%\release\binaries\Jorbique-VPN-Extended.exe
echo   %ROOT%\release\binaries\Jorbique-VPN-Extended-windows.zip
goto done

:fail
pause
exit /b 1

:done
pause
exit /b 0
