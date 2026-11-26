const { app, BrowserWindow, Menu } = require("electron");

app.disableHardwareAcceleration();
app.whenReady().then(async () => {
    Menu.setApplicationMenu(null);
    const window = new BrowserWindow({
        width: 320,
        height: 160,
        useContentSize: true,
        backgroundColor: "#00cc44",
    });
    await window.loadURL("data:text/html,<title>SmokeElectron</title>");
});
app.on("window-all-closed", () => app.quit());
