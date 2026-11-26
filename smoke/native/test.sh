#!/usr/bin/env bash
# Child shells expand the snippets passed to bash -c and check_sh.
# shellcheck disable=SC2016
set -uo pipefail
ulimit -c 0
# shellcheck source=smoke/lib.sh
. ../lib.sh

work=$(mktemp -d) || exit 1
trap 'rm -rf "$work"' EXIT
cp -R fixtures/. "$work" || exit 1
cd "$work" || exit 1

export DEBUGINFOD_URLS='' GOPROXY=https://proxy.golang.org
export PATH="$HOME/.cargo/bin:$PATH" HOMEBREW_NO_ANALYTICS=1
export GOMAXPROCS=2 CARGO_BUILD_JOBS=2

run_hello() {
    local output
    output=$(timeout 30 "$@") || return
    [[ "$output" == native-smoke ]]
}

glibc() {
    timeout 60 "$1" -fPIC -shared plugin.c -o plugin.so &&
        timeout 60 "$1" -Wall -Wextra -pthread posix.c -ldl -o posix &&
        timeout 30 ./posix
}

build_hello() {
    timeout 60 "$@" hello.c -o static-hello &&
        readelf -l static-hello > static-hello.headers &&
        ! grep -q INTERP static-hello.headers &&
        run_hello ./static-hello
}

check 'GCC glibc locale, iconv, dlopen, pthreads and getaddrinfo' glibc gcc
full_only check 'Clang glibc locale, iconv, dlopen, pthreads and getaddrinfo' glibc clang
check 'GCC static glibc binary' build_hello gcc -static
full_only check 'GCC static musl binary' build_hello musl-gcc -static
check_sh 'Make builds and runs a dynamic C program' '
    timeout 60 make && readelf -l hello > hello.headers &&
    grep -q INTERP hello.headers && [[ $(./hello) == native-smoke ]]'
full_only check_sh 'CMake with Ninja builds and runs a C program' '
    timeout 60 cmake -G Ninja -S . -B cmake-out &&
    timeout 60 cmake --build cmake-out &&
    [[ $(./cmake-out/hello) == native-smoke ]]'
full_only check_sh 'Meson with Ninja builds and runs a C program' '
    timeout 60 meson setup meson-out &&
    timeout 60 meson compile -C meson-out &&
    [[ $(./meson-out/hello) == native-smoke ]]'
full_only check_sh 'Autotools regenerates, configures and builds a C program' '
    mkdir autotools && cp configure.ac Makefile.am hello.c autotools/ &&
    cd autotools && timeout 60 autoreconf -fi && timeout 60 ./configure &&
    timeout 60 make -B && [[ $(./hello) == native-smoke ]]'

modern_cpp() {
    timeout 90 "$1" "-std=$2" -Wall -Wextra -pthread modern.cpp -o modern &&
        timeout 30 ./modern
}

check 'GCC C++20 format, filesystem, jthread and ranges' modern_cpp g++ c++20
full_only check 'GCC C++23 format, filesystem, jthread and ranges' modern_cpp g++ c++23
full_only check 'Clang C++23 with libstdc++' modern_cpp clang++ c++23

build_libraries() {
    local flags
    flags=$(pkg-config --cflags --libs zlib libcrypto libcurl sqlite3 libuv ncurses) || return
    # shellcheck disable=SC2086 # pkg-config emits compiler arguments.
    timeout 60 gcc -Wall -Wextra libraries.c $flags -o libraries
}

check 'pkg-config resolves and links C library dependencies' build_libraries
probe_library() {
    [[ -x ./libraries ]] || build_libraries || return
    timeout 30 ./libraries "$1"
}
check 'zlib compresses and restores a payload' probe_library zlib
check 'OpenSSL libcrypto computes SHA-256' probe_library crypto
check 'libcurl makes a certificate-verified HTTPS request' probe_library https
check 'SQLite C API creates, inserts and queries' probe_library sqlite
check 'libuv schedules and closes a timer' probe_library uv
check 'ncurses initializes an xterm screen and reads a drawn cell' probe_library curses

full_only check 'GDB captures SIGABRT and the fixture backtrace' bash -o pipefail -c '
    timeout 60 gcc -g -O0 crash.c -o crash &&
    timeout 45 gdb -nx -batch -ex run -ex bt ./crash > gdb.log 2>&1 &&
    cat gdb.log && grep -q SIGABRT gdb.log && grep -q crash_fixture gdb.log'
full_only check 'GDB writes and reopens a core dump' bash -c '
    { test -x ./crash || timeout 60 gcc -g -O0 crash.c -o crash; } &&
    timeout 45 gdb -nx -batch -ex run -ex "generate-core-file crash.core" ./crash &&
    test -s crash.core &&
    timeout 45 gdb -nx -batch -ex bt ./crash crash.core > core.log 2>&1 &&
    cat core.log && grep -q crash_fixture core.log'
memcheck() {
    [[ -x ./hello ]] || timeout 60 make || return
    run_hello valgrind --error-exitcode=1 --leak-check=full ./hello
}
full_only check 'Valgrind Memcheck runs a dynamic C binary' memcheck
check 'strace observes a write syscall' bash -c '
    { test -x ./hello || timeout 60 make; } &&
    timeout 30 strace -o syscalls.log -e trace=write ./hello &&
    grep -q "write(1, .*native-smoke" syscalls.log'
full_only check 'ltrace observes a libc call' bash -c '
    { test -x ./hello || timeout 60 make; } &&
    timeout 30 ltrace -o libc.log -e puts ./hello &&
    cat libc.log && grep -q "puts(.*native-smoke" libc.log'

check 'AddressSanitizer and UBSan run a threaded C fixture' bash -c '
    timeout 60 gcc -g -O1 -fsanitize=address,undefined -fno-sanitize-recover=all \
        -pthread posix.c -ldl -o sanitized &&
    timeout 30 ./sanitized'
full_only check 'ThreadSanitizer runs a joined pthread' bash -c '
    timeout 60 gcc -g -O1 -fsanitize=thread -pthread posix.c -ldl -o thread-sanitized &&
    TSAN_OPTIONS=halt_on_error=1 timeout 30 ./thread-sanitized'

if [[ ${SMOKE_LEVEL:-full} == full ]]; then
    for event in task-clock cycles; do
        smoke_matches "perf counts $event" || continue
        if timeout 20 sudo -n perf stat -x ';' -e "$event" -- true > perf.log 2>&1 &&
            grep -Eq "^[0-9.]+;[^;]*;$event;" perf.log; then
            pass "perf counts $event"
        elif grep -Eqi 'not supported|permission|not permitted' perf.log; then
            skip "perf $event unavailable: $(tail -n 8 perf.log | xargs)"
        else
            fail "perf $event did not return a count"
            cat perf.log
        fi
    done
fi

go_check() {
    (cd go && timeout 300 mise exec go@1.25.14 -- go "$@")
}

go_download() {
    (cd go && GOFLAGS=-modcacherw GOMODCACHE="$work/go-download" timeout 120 mise exec go@1.25.14 -- go mod download -json all > modules.json &&
        grep -q '"Path": "github.com/google/uuid"' modules.json)
}

go_cross() {
    local os=$1 arch=$2 format=$3 binary="go-$1-$2"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go_check build -o "../$binary" &&
        file "$binary" | grep -Eq "$format"
}

full_only check 'Go fetches a pinned module through the module proxy' go_download
check 'Go tests call C through cgo' go_check test -count=1 ./...
full_only check 'Go race runtime with cgo' go_check test -race -count=1 ./...
full_only check 'Go cross-compiles a Linux amd64 ELF' go_cross linux amd64 'ELF.*x86-64'
full_only check 'Go cross-compiles a Darwin arm64 Mach-O' go_cross darwin arm64 'Mach-O.*arm64'

run_amd64() {
    [[ -x ./go-linux-amd64 ]] || go_cross linux amd64 'ELF.*x86-64' || return
    [[ $(timeout 30 "$@" ./go-linux-amd64) == 00000000-0000-0000-0000-000000000000 ]]
}
binfmt_check() {
    grep -qx enabled /proc/sys/fs/binfmt_misc/qemu-x86_64 && run_amd64 env
}
full_only check 'QEMU interprets the Linux amd64 executable' run_amd64 qemu-x86_64
if [[ ${SMOKE_LEVEL:-full} == full ]]; then
    if [[ $(uname -m) == aarch64 ]]; then
        check 'binfmt transparently dispatches Linux amd64 to QEMU' binfmt_check
    elif smoke_matches 'binfmt transparently dispatches Linux amd64 to QEMU'; then
        skip 'binfmt dispatch requires a non-amd64 guest'
    fi
fi

cargo_check() {
    (cd rust && timeout 300 cargo +1.92.0 "$@")
}

rust_server() (
    cargo_check build --locked || exit 1
    timeout 40 rust/target/debug/native-smoke > server.port &
    server=$!
    trap 'kill "$server" 2>/dev/null || true; wait "$server" 2>/dev/null || true' EXIT
    for ((attempt = 0; attempt < 50; attempt++)); do
        if [[ -s server.port ]]; then
            read -r port < server.port || exit 1
            [[ "$port" =~ ^[0-9]+$ ]] || exit 1
            response=$(curl -fsS --max-time 5 "http://127.0.0.1:$port/") || exit 1
            [[ "$response" == native-smoke ]]
            exit
        fi
        kill -0 "$server" || exit 1
        sleep 0.1
    done
    exit 1
)

check 'Cargo fetches crates and runs a Tokio test' cargo_check test --locked
full_only check 'rustfmt checks the Axum project' cargo_check fmt -- --check
full_only check 'Clippy lints the Axum project' cargo_check clippy --locked --all-targets -- -D warnings
full_only check 'Tokio and Axum serve a real HTTP request on guest loopback' rust_server
full_only check 'Zig compiles and runs a libc-linked executable' bash -c '
    timeout 120 mise exec zig@0.15.2 -- zig build-exe -lc hello.zig &&
    [[ $(timeout 30 ./hello) == native-smoke ]]'
full_only check 'Zig runs unit tests' timeout 120 mise exec zig@0.15.2 -- zig test hello.zig

wasi() {
    local release=36.0.0 architecture
    case "$(uname -m)" in
        aarch64) architecture=aarch64 ;;
        x86_64) architecture=x86_64 ;;
        *) return 1 ;;
    esac
    local archive="wasmtime-v$release-$architecture-linux"
    curl -fsSL --max-time 120 "https://github.com/bytecodealliance/wasmtime/releases/download/v$release/$archive.tar.xz" \
        -o wasmtime.tar.xz &&
        timeout 45 tar -xf wasmtime.tar.xz "$archive/wasmtime" &&
        timeout 90 rustc +1.92.0 --target wasm32-wasip1 wasi.rs -o hello.wasm &&
        run_hello "./$archive/wasmtime" run hello.wasm
}

full_only check 'Rust WASI module prints through Wasmtime' wasi

brew_install() {
    local brew=/home/linuxbrew/.linuxbrew/bin/brew
    if [[ ! -x "$brew" ]]; then
        curl -fsSL --max-time 60 https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh -o brew-install.sh &&
            NONINTERACTIVE=1 timeout 300 bash brew-install.sh || return
    fi
    HOMEBREW_NO_AUTO_UPDATE=1 timeout 300 "$brew" install hello &&
        [[ $("$brew" --prefix hello)/bin/hello -ef /home/linuxbrew/.linuxbrew/bin/hello ]] &&
        [[ $(timeout 30 /home/linuxbrew/.linuxbrew/bin/hello) == 'Hello, world!' ]]
}

nix_run() {
    [[ $(timeout 300 nix --extra-experimental-features 'nix-command flakes' run \
        github:NixOS/nixpkgs/b6018f87da91d19d0ab4cf979885689b469cdd41#hello -- --greeting=native-smoke) == native-smoke ]]
}

apt_lifecycle() (
    set -e
    local package=hello
    if dpkg-query -W -f='${db:Status-Status}' "$package" 2>/dev/null | grep -qx installed; then
        echo "$package already installed; preserving existing apt state"
        exit 2
    fi
    trap 'sudo -n timeout 60 apt-mark unhold hello; sudo -n timeout 120 apt-get -y purge hello' EXIT
    sudo -n timeout 120 apt-get -y install "$package" || exit 1
    [[ $(/usr/bin/hello --greeting=native-smoke) == native-smoke ]] || exit 1
    sudo -n timeout 30 apt-mark hold "$package" || exit 1
    apt-mark showhold | grep -qx "$package" || exit 1
    sudo -n timeout 30 apt-mark unhold "$package" || exit 1
    sudo -n timeout 120 apt-get -y purge "$package" || exit 1
    [[ ! -e /usr/bin/hello ]] || exit 1
    trap - EXIT
)

apt_source() (
    mkdir -p apt-sources/lists/partial apt-source || exit 1
    local sources=/etc/apt/sources.list.d/ubuntu.sources
    sed -n 's/^Types: deb$/Types: deb-src/; p' "$sources" > apt-sources/ubuntu.sources || exit 1
    local options=(-o "Dir::Etc::sourcelist=$work/apt-sources/ubuntu.sources"
                   -o Dir::Etc::sourceparts=- -o "Dir::State::lists=$work/apt-sources/lists")
    timeout 180 apt-get "${options[@]}" update || exit 1
    cd apt-source || exit 1
    timeout 120 apt-get "${options[@]}" source hello || exit 1
    compgen -G 'hello-*/debian/control' > /dev/null
)

full_only check 'Homebrew installs a formula and runs it' brew_install
full_only check 'Nix resolves a flake and runs an executable' nix_run
if dpkg-query -W -f='${db:Status-Status}' hello 2>/dev/null | grep -qx installed; then
    if smoke_matches 'apt installs, holds, unholds and purges a package'; then
        skip 'apt lifecycle would alter the existing hello package'
    fi
else
    check 'apt installs, holds, unholds and purges a package' apt_lifecycle
fi
full_only check 'apt fetches and unpacks a source package using isolated indexes' apt_source

finish
