#!/usr/bin/env bash
# shellcheck source=smoke/runtimes/fixture.sh
source ./fixture.sh

rails_app() {
    export GEM_HOME="$PWD/gems"
    export PATH="$GEM_HOME/bin:$PATH"
    gem install --no-document rails -v 8.1.2
    rails new app --minimal --skip-git --skip-bundle
    cd app
    sed -i 's/^gem "rails".*/gem "rails", "8.1.2"/' Gemfile
    bundle install
    bin/rails generate scaffold Widget name:string
    bin/rails db:migrate
    bin/rails test test/controllers/widgets_controller_test.rb
    bin/rails runner 'Widget.create!(name: "homebox-rails")'
    expect_http /widgets homebox-rails bin/rails server --binding=127.0.0.1 --port=18765
}

java_app() {
    cat > Smoke.java <<'JAVA'
import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;

public class Smoke {
    public static void main(String[] args) throws Exception {
        var server = HttpServer.create(new InetSocketAddress("127.0.0.1", 18765), 0);
        server.createContext("/", exchange -> {
            byte[] body = "homebox-java".getBytes(StandardCharsets.UTF_8);
            exchange.sendResponseHeaders(200, body.length);
            try (var output = exchange.getResponseBody()) {
                output.write(body);
            }
        });
        server.start();
    }
}
JAVA
    javac Smoke.java
    expect_http / homebox-java java Smoke
}

maven_app() {
    cat > pom.xml <<'XML'
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>dev.homebox</groupId><artifactId>smoke</artifactId><version>1.0</version>
  <properties>
    <maven.compiler.release>21</maven.compiler.release>
    <maven.compiler.source>21</maven.compiler.source>
    <maven.compiler.target>21</maven.compiler.target>
  </properties>
  <dependencies>
    <dependency>
      <groupId>junit</groupId><artifactId>junit</artifactId><version>4.13.2</version><scope>test</scope>
    </dependency>
  </dependencies>
</project>
XML
    mkdir -p src/test/java
    cat > src/test/java/SmokeTest.java <<'JAVA'
import org.junit.Test;
import static org.junit.Assert.assertEquals;

public class SmokeTest {
    @Test public void computesAnswer() {
        assertEquals(42, 6 * 7);
    }
}
JAVA
    mvn -B package
    grep -F 'Tests run: 1, Failures: 0, Errors: 0, Skipped: 0' target/surefire-reports/SmokeTest.txt
}

gradle_app() {
    printf 'rootProject.name = "homebox-smoke"\n' > settings.gradle
    cat > build.gradle <<'GRADLE'
plugins {
    id 'org.jetbrains.kotlin.jvm' version '2.2.21'
}
repositories {
    mavenCentral()
}
dependencies {
    testImplementation 'junit:junit:4.13.2'
}
GRADLE
    mkdir -p src/test/kotlin
    cat > src/test/kotlin/SmokeTest.kt <<'KOTLIN'
import org.junit.Assert.assertEquals
import org.junit.Test

class SmokeTest {
    @Test fun computesAnswer() {
        assertEquals(42, 6 * 7)
    }
}
KOTLIN
    gradle --no-daemon -Dorg.gradle.jvmargs=-Xmx512m -Pkotlin.compiler.execution.strategy=in-process build
    grep -F 'tests="1"' build/test-results/test/TEST-SmokeTest.xml
}

php_app() {
    composer require --no-interaction 'psr/log:3.0.2'
    cat > index.php <<'PHP'
<?php
require 'vendor/autoload.php';
$logger = new Psr\Log\NullLogger();
$logger->info('smoke');
echo 'homebox-php';
PHP
    expect_http / homebox-php php -S 127.0.0.1:18765
}

elixir_app() {
    mix new app
    cd app
    mix test
    mix run -e 'true = (App.hello() == :world)'
}

dotnet_app() {
    export DOTNET_CLI_TELEMETRY_OPTOUT=1
    dotnet new console --output console
    dotnet run --project console | grep -F 'Hello, World!'
    dotnet new webapi --no-https --output api
    cat > api/Program.cs <<'CS'
var app = WebApplication.CreateBuilder(args).Build();
app.MapGet("/", () => "homebox-dotnet");
app.Run();
CS
    dotnet publish api --output published
    expect_http / homebox-dotnet dotnet published/api.dll --urls http://127.0.0.1:18765
}

agents() {
    claude --version | grep -E '[0-9]+\.[0-9]+\.[0-9]+'
    codex --version | grep -E '[0-9]+\.[0-9]+\.[0-9]+'
}

"$1"
