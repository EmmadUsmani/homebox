const { createServer } = require("node:http");
const escape = require("escape-html");

createServer((_request, response) => {
  response.end(escape("node:<42>"));
}).listen(8080, "0.0.0.0");
