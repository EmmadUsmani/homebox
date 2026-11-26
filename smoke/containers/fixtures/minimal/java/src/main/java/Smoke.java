import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;

public class Smoke {
    public static void main(String[] args) throws Exception {
        var server = HttpServer.create(new InetSocketAddress(8080), 0);
        server.createContext("/", exchange -> {
            byte[] body = "java:42".getBytes(StandardCharsets.UTF_8);
            exchange.sendResponseHeaders(200, body.length);
            try (var response = exchange.getResponseBody()) {
                response.write(body);
            }
        });
        server.start();
    }
}
