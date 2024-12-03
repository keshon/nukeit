@echo off

rem
rem BUILD
rem

rem Get Go version
for /f "tokens=3" %%i in ('go version') do set GO_VERSION=%%i

rem Get the build date
for /f "tokens=*" %%a in ('powershell -command "Get-Date -UFormat '%%Y-%%m-%%dT%%H:%%M:%%SZ'"') do set BUILD_DATE=%%a

rem Build command
go build -o nukeit.exe -ldflags "-X github.com/keshon/nukeit/internal/version.BuildDate=%BUILD_DATE% -X github.com/keshon/nukeit/internal/version.GoVersion=%GO_VERSION%" cmd\nukeit\nukeit.go