# Runtime smoke tests

Quick uses the guest profile’s Node and Python to exercise the Node test
runner, npm/pnpm/Yarn installs, Python wheels and a compiled extension,
and starts the two coding-agent CLIs. Full installs the extra runtimes and adds Bun, Deno, Node add-ons and web
frameworks, uv/pipx, Python services and notebooks, Rails, Java with Maven
and Gradle, PHP, Elixir and .NET.

Run `smoke/run.sh NAME runtimes` from the host against a disposable Ubuntu VM.
Setup installs Ruby, Bun, Deno, a Java 21 JDK, Maven, Gradle and the .NET SDK in
the guest user's global mise config. It uses apt for PHP, Composer, Elixir,
Erlang and the native build headers. Node, Python, uv and the coding agent CLIs
come from the guest profile.

The checks install dependencies, compile native code, run tests, migrate
SQLite and exercise production and development HTTP servers. Gradle builds
the Kotlin fixture. The Java HTTP server uses the JDK's built-in server.
Python includes a second interpreter and headless Jupyter execution. The
coding-agent checks use version commands and need no logins.

Fixtures run in temporary guest directories and HTTP servers bind to guest
loopback on port 18765, which must be free. Download caches remain available
for reruns. No ports need to be published. Installation and test commands
have deadlines. Allow up to 30 minutes for setup on a cold guest, especially
when compiling Ruby, plus time to download project dependencies and run
the builds. The checks require internet access to the package registries.
