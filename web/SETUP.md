# Web UI Setup Guide

## Installing Node.js (if not already installed)

### Windows

1. **Download Node.js:**
   - Go to [https://nodejs.org/](https://nodejs.org/)
   - Download the **LTS (Long Term Support)** version (recommended)
   - Choose the Windows Installer (.msi) for your system (64-bit or 32-bit)

2. **Install Node.js:**
   - Run the downloaded installer
   - Follow the installation wizard (accept defaults)
   - Make sure "Add to PATH" is checked (it should be by default)

3. **Verify Installation:**
   Open a new PowerShell window and run:
   ```powershell
   node --version
   npm --version
   ```
   You should see version numbers (e.g., `v20.10.0` and `10.2.3`)

4. **If commands still don't work:**
   - Close and reopen your terminal/PowerShell
   - Or restart your computer to refresh environment variables

### Alternative: Using a Package Manager

**Using Chocolatey (if installed):**
```powershell
choco install nodejs
```

**Using winget (Windows 10/11):**
```powershell
winget install OpenJS.NodeJS.LTS
```

## After Installing Node.js

1. **Navigate to the web directory:**
   ```powershell
   cd C:\Users\pares\code\talkback\web
   ```

2. **Install dependencies:**
   ```powershell
   npm install
   ```

3. **Start the development server:**
   ```powershell
   npm run dev
   ```

4. **The app will open automatically** at `http://localhost:3000`

## Troubleshooting

**"npm is not recognized" after installing Node.js:**
- Close and reopen your terminal/PowerShell
- Verify Node.js is in your PATH: `$env:PATH -split ';' | Select-String node`
- If Node.js isn't in PATH, restart your computer

**Port 3000 already in use:**
- Vite will automatically try the next available port (3001, 3002, etc.)
- Check the terminal output for the actual port

**API connection errors:**
- Make sure the Go API server is running: `go run ./cmd/api`
- Verify the API Base URL in the web UI (debug panel) matches your server, or set `VITE_API_BASE_URL` in `web/.env`
