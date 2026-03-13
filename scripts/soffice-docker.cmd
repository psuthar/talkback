@echo off
REM Run soffice via TalkBack Docker image. Pass-through all arguments.
REM Set TALKBACK_SOFFICE_CMD to the full path of this .cmd (e.g. C:\Users\...\talkback\scripts\soffice-docker.cmd)
powershell -ExecutionPolicy Bypass -NoProfile -File "%~dp0soffice-docker.ps1" %*
