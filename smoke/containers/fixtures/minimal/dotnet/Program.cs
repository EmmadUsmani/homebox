var builder = WebApplication.CreateBuilder(args);
var app = builder.Build();
app.MapGet("/", () => "dotnet:42");
app.Run("http://0.0.0.0:8080");
