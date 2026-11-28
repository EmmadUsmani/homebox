const { copyFileSync, mkdirSync, writeFileSync } = require("node:fs");
const { spawnSync } = require("node:child_process");
const { join } = require("node:path");

const desktop = join(__dirname, "..");

function run(command, args, cwd, env = process.env) {
    const result = spawnSync(command, args, { cwd, env, stdio: "inherit" });
    if (result.error) throw result.error;
    if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed.`);
}

function build() {
    const mac = process.platform === "darwin";
    if (!mac && process.platform !== "linux") {
        throw new Error("The desktop build supports macOS and Linux.");
    }
    const bundle = join(desktop, "dist", mac ? "homebox.app" : "homebox-linux");
    const executables = mac ? join(bundle, "Contents", "MacOS") : bundle;
    const executable = join(executables, "homebox");
    const frontend = join(desktop, "frontend");
    run("npm", ["ci"], frontend);
    run("npm", ["run", "build"], frontend);

    mkdirSync(executables, { recursive: true });
    run("go", ["build", "-o", join(executables, "box"), "./cmd/box"], join(desktop, ".."),
        { ...process.env, CGO_ENABLED: "0" });
    const flags = ["build", "-tags", "production"];
    const env = { ...process.env, CGO_ENABLED: "1" };
    if (mac) {
        flags.push("-ldflags=-extldflags=-mmacosx-version-min=15.0");
        env.MACOSX_DEPLOYMENT_TARGET = "15.0";
    }
    run("go", [...flags, "-o", executable, "."], desktop, env);
    if (mac) {
        copyFileSync(join(desktop, "Info.plist"), join(bundle, "Contents", "Info.plist"));
    } else {
        // Exec has a quoting layer in addition to the desktop entry's escaping.
        const command = executable.replace(/[\\"$`]/g, "\\$&").replace(/%/g, "%%");
        const exec = `"${command}"`.replaceAll("\\", "\\\\").replaceAll("\n", "\\n").replaceAll("\r", "\\r");
        writeFileSync(join(bundle, "homebox.desktop"), [
            "[Desktop Entry]",
            "Type=Application",
            "Name=homebox",
            `Exec=${exec}`,
            "Terminal=false",
            "Categories=Development;",
            "",
        ].join("\n"));
    }
    const resources = mac ? join(bundle, "Contents", "Resources") : bundle;
    mkdirSync(resources, { recursive: true });
    for (const font of ["inter", "jetbrains-mono"]) {
        const licence = `${font}-OFL.txt`;
        copyFileSync(join(frontend, "src", "fonts", licence), join(resources, licence));
    }
    return bundle;
}

module.exports = { build };
if (require.main === module) console.log(build());
