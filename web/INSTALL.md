# Installing Dependencies - Windows PowerShell Fix

## Problem

If you see this error:
```
npm : File C:\Program Files\nodejs\npm.ps1 cannot be loaded because running scripts is disabled on this system.
```

This is because PowerShell's execution policy is blocking npm scripts.

## Solution Options

### Option 1: Change Execution Policy (Recommended)

Run PowerShell as Administrator and execute:

```powershell
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
```

This allows scripts to run for your user account. You'll be prompted to confirm - type `Y` and press Enter.

**Then verify:**
```powershell
Get-ExecutionPolicy
```
Should show `RemoteSigned` or `Unrestricted`

**Now you can run:**
```powershell
cd web
npm install
```

### Option 2: Use Command Prompt (CMD) Instead

If you prefer not to change PowerShell settings:

1. Open **Command Prompt** (cmd.exe) instead of PowerShell
2. Navigate to the web directory:
   ```cmd
   cd C:\Users\pares\code\talkback\web
   ```
3. Run npm commands:
   ```cmd
   npm install
   npm run dev
   ```

### Option 3: Bypass for Single Command

You can bypass the policy for a single command:

```powershell
powershell -ExecutionPolicy Bypass -Command "cd web; npm install"
```

## After Fixing

Once npm works, continue with:

```powershell
cd web
npm install
npm run dev
```

The web UI will start at `http://localhost:3000`
