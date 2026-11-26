# Native development smoke tests

After `box new` finishes provisioning, run `smoke/run.sh NAME native`.
Use a disposable Ubuntu VM. Allow about 15 minutes for the first run on a
2-CPU, 4-GiB guest, with internet access to the toolchain and package registries.

- Compile and run GCC and Clang C fixtures using glibc locales, iconv, pthreads,
  local name resolution and dynamic loading. Build static glibc and musl
  executables and exercise C++20 and C++23 library features.
- Build a C program with Make, CMake/Ninja, Meson and Autotools. Resolve
  development libraries through pkg-config, then exercise zlib, libcrypto,
  libcurl over HTTPS, SQLite, libuv and ncurses through their C APIs.
- Capture a GDB backtrace and reopen a GDB-generated core. Run Memcheck,
  strace, ltrace, ASan, UBSan and TSan, and ask perf for software and hardware
  counters.
- Fetch a Go module, run cgo and race tests, cross-compile Linux/amd64 and
  Darwin/arm64 binaries, and run the Linux artifact via QEMU and binfmt.
- Fetch crates, test and lint a Rust project, send an HTTP request to its
  Axum/Tokio server, and run a separately compiled WASI program in Wasmtime.
  Build and test a Zig program.
- Install and run GNU Hello through Homebrew, Nix and apt. Exercise apt holds,
  removal and source extraction with temporary source indexes.

The Ubuntu profile already provides the baseline shared by other workloads
(GCC, G++, Make, pkg-config and UTF-8 locale data). Clang, alternate build
systems, debugger and profiling packages, development headers, emulators and
additional runtimes are installed for this area. Setup also installs the
Ubuntu Nix daemon package. The Homebrew check may install Homebrew under
`/home/linuxbrew`. Runtime and package-manager caches remain available for
reruns. Build outputs and the HTTP server are cleaned up after the run.

The GDB-generated core exercises offline crash analysis without changing
Ubuntu's system-wide crash handler. Hardware counters are skipped when the
guest kernel reports that they are unavailable. The apt lifecycle check
skips a preinstalled `hello` package to preserve its existing state.
