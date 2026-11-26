const { app, BrowserWindow, Menu } = require("electron");
const assertSandboxedRenderer = require("./renderer-sandbox.cjs");

app.disableHardwareAcceleration();
app.whenReady().then(async () => {
    Menu.setApplicationMenu(null);
    const window = new BrowserWindow({
        width: 320,
        height: 160,
        useContentSize: true,
        backgroundColor: "#00cc44",
        show: false,
        webPreferences: { sandbox: true },
    });
    await window.loadURL("data:text/html,<title>SmokeElectron</title>");
    assertSandboxedRenderer(window.webContents.getOSProcessId());
    window.show();
}).catch((error) => {
    console.error(error);
    app.exit(1);
});
app.on("window-all-closed", () => app.quit());
