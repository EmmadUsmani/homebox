const { copyFileSync, mkdirSync } = require("node:fs");
const { spawnSync } = require("node:child_process");
const { join } = require("node:path");

const desktop = join(__dirname, "..");
const bundle = join(desktop, "dist", "homebox.app");
const executables = join(bundle, "Contents", "MacOS");
const executable = join(executables, "homebox");

function run(command, args, cwd, env = process.env) {
    const result = spawnSync(command, args, { cwd, env, stdio: "inherit" });
    if (result.error) throw result.error;
    if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed.`);
}

function build() {
    if (process.platform !== "darwin") {
        throw new Error("The desktop build packages a macOS app.");
    }
    const frontend = join(desktop, "frontend");
    run("npm", ["ci"], frontend);
    run("npm", ["run", "build"], frontend);

    mkdirSync(executables, { recursive: true });
    run("go", ["build", "-o", join(executables, "box"), "./cmd/box"], join(desktop, ".."),
        { ...process.env, CGO_ENABLED: "0" });
    run("go", [
        "build", "-tags", "production", "-ldflags=-extldflags=-mmacosx-version-min=15.0",
        "-o", executable, ".",
    ], desktop, { ...process.env, CGO_ENABLED: "1", MACOSX_DEPLOYMENT_TARGET: "15.0" });
    copyFileSync(join(desktop, "Info.plist"), join(bundle, "Contents", "Info.plist"));
    const resources = join(bundle, "Contents", "Resources");
    mkdirSync(resources, { recursive: true });
    for (const font of ["inter", "jetbrains-mono"]) {
        const licence = `${font}-OFL.txt`;
        copyFileSync(join(frontend, "src", "fonts", licence), join(resources, licence));
    }
    return bundle;
}

module.exports = { build };
if (require.main === module) console.log(build());
