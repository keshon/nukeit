@echo off
setlocal

rem
rem BUILD
rem

rem Get Go version
for /f "tokens=3" %%i in ('go version') do set GO_VERSION=%%i

rem Get the build date
for /f "tokens=*" %%a in ('powershell -command "Get-Date -UFormat '%%Y-%%m-%%dT%%H:%%M:%%SZ'"') do set BUILD_DATE=%%a

rem Define output binary name
set OUTPUT_BIN=nukeit.exe

rem Build the binary
go build -ldflags "-X github.com/keshon/nukeit/internal/version.BuildDate=%BUILD_DATE% -X github.com/keshon/nukeit/internal/version.GoVersion=%GO_VERSION%" -o %OUTPUT_BIN% cmd\nukeit\nukeit.go

if errorlevel 1 (
    echo Build failed. Congratulations, you are useless.
    exit /b 1
)

rem Execute the binary
echo Running %OUTPUT_BIN%...
%OUTPUT_BIN%

endlocal
