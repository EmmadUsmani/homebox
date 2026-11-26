#!/usr/bin/env bash
set -eu

# shellcheck source=smoke/lib.sh
source ../lib.sh

run() {
    local limit=$1 suite=$2 scenario=$3 label=$4
    check "$label" timeout --kill-after=10s "$limit" mise exec -- bash "$suite.sh" "$scenario"
}

run 3m javascript node_test 'Node runs its built-in test runner'
run 5m javascript package_managers 'npm, pnpm and Yarn install and load a dependency'
full_only run 8m javascript native_addon 'node-gyp compiles better-sqlite3 and Node queries it'
full_only run 5m javascript vite 'TypeScript type-checks and Vite builds a web app'
full_only run 8m javascript next_app 'Next.js builds and serves a production page'
full_only run 5m javascript bun_app 'Bun installs dependencies, runs tests and serves HTTP'
full_only run 5m javascript deno_app 'Deno runs a task and tests an npm import'

run 5m python pip_app 'Python venv installs wheels and runs pytest'
full_only run 6m python uv_app 'uv creates, syncs and runs a project, and installs a second Python'
full_only run 5m python tools 'uvx and pipx run tools in isolated environments'
run 5m python native_extension 'Python builds, installs and imports a C extension'
full_only run 5m python fastapi_app 'FastAPI responds over HTTP with Uvicorn'
full_only run 5m python django_app 'Django migrates SQLite and serves a page'
full_only run 6m python notebook 'Jupyter executes a notebook and saves its output'

full_only run 10m other rails_app 'Bundler installs Rails, which migrates, tests and serves a scaffold'
full_only run 3m other java_app 'The mise JDK compiles Java and serves HTTP'
full_only run 8m other maven_app 'Maven resolves dependencies and runs a unit test'
full_only run 10m other gradle_app 'Gradle resolves dependencies and runs a Kotlin unit test'
full_only run 5m other php_app 'Composer installs a package and PHP serves its output'
full_only run 3m other elixir_app 'Mix creates, builds and tests an application'
full_only run 5m other dotnet_app '.NET creates, runs and serves console and web API projects'
run 2m other agents 'Claude Code and Codex start and print versions'

finish
