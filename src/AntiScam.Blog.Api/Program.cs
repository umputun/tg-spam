using AntiScam.Blog.Api;
using AntiScam.Blog.Api.Data;
using AntiScam.Blog.Api.Models;
using AntiScam.Blog.Api.Services;
using System.Net;

var builder = WebApplication.CreateBuilder(args);

var configuredWorkspace = builder.Configuration["Workspace:RootPath"];
var workspacePath = string.IsNullOrWhiteSpace(configuredWorkspace)
    ? builder.Environment.ContentRootPath
    : ResolvePath(configuredWorkspace, builder.Environment.ContentRootPath);

var configuredDatabasePath = builder.Configuration["Blog:DatabasePath"];
var environmentDatabasePath = Environment.GetEnvironmentVariable("ANTISCAM_BLOG_DB");
var databasePath = !string.IsNullOrWhiteSpace(environmentDatabasePath)
    ? ResolvePath(environmentDatabasePath, workspacePath)
    : !string.IsNullOrWhiteSpace(configuredDatabasePath)
        ? ResolvePath(configuredDatabasePath, workspacePath)
        : Path.Combine(workspacePath, "data", "antiscam-blog.sqlite");

var noSqlOptions = builder.Configuration.GetSection("NoSql").Get<NoSqlDatabaseOptions>()
    ?? new NoSqlDatabaseOptions();
var mongoConnectionString = Environment.GetEnvironmentVariable("ANTISCAM_MONGO_CONNECTION_STRING");
if (!string.IsNullOrWhiteSpace(mongoConnectionString))
{
    noSqlOptions = noSqlOptions with { ConnectionString = mongoConnectionString };
}
var httpsOptions = builder.Configuration.GetSection("Https").Get<HttpsOptions>() ?? new HttpsOptions();
httpsOptions = httpsOptions with { CertificatePath = ResolvePath(httpsOptions.CertificatePath, workspacePath) };
var networkOptions = builder.Configuration.GetSection("Network").Get<NetworkOptions>() ?? new NetworkOptions();
var httpsPassword = Environment.GetEnvironmentVariable("ANTISCAM_HTTPS_CERT_PASSWORD");
if (!string.IsNullOrWhiteSpace(httpsPassword)) httpsOptions = httpsOptions with { CertificatePassword = httpsPassword };
if (httpsOptions.Enabled)
{
    if (!IPAddress.TryParse(httpsOptions.ListenAddress, out var listenAddress))
        throw new InvalidOperationException("Https:ListenAddress must be a valid IP address.");
    if (!File.Exists(httpsOptions.CertificatePath) || string.IsNullOrEmpty(httpsOptions.CertificatePassword))
        throw new InvalidOperationException("HTTPS requires an existing PFX certificate and ANTISCAM_HTTPS_CERT_PASSWORD.");
    builder.WebHost.ConfigureKestrel(kestrel => kestrel.Listen(listenAddress, httpsOptions.Port,
        listen => listen.UseHttps(httpsOptions.CertificatePath, httpsOptions.CertificatePassword)));
}
else if (networkOptions.BindToLan)
{
    builder.WebHost.UseUrls($"http://0.0.0.0:{networkOptions.HttpPort}");
}

builder.Services.AddSingleton(new WorkspaceOptions(workspacePath));
builder.Services.AddSingleton(new BlogDatabaseOptions(databasePath));
builder.Services.AddSingleton(noSqlOptions);
builder.Services.AddSingleton<ISlugGenerator, SlugGenerator>();
builder.Services.AddSingleton<IRiskAnalyzer, RiskAnalyzer>();
builder.Services.AddSingleton<IBlockExplanationProvider, PythonAiBlockExplanationProvider>();
builder.Services.AddSingleton<IScamIncidentStore>(serviceProvider =>
    noSqlOptions.Enabled
        ? new MongoScamIncidentStore(
            noSqlOptions,
            serviceProvider.GetRequiredService<ILogger<MongoScamIncidentStore>>())
        : new NullScamIncidentStore());
builder.Services.AddSingleton<IBlogRepository, SqliteBlogRepository>();

var app = builder.Build();

app.UseDefaultFiles();
app.UseStaticFiles(new StaticFileOptions
{
    OnPrepareResponse = context =>
    {
        context.Context.Response.Headers.CacheControl = "no-store, no-cache, must-revalidate";
        context.Context.Response.Headers.Pragma = "no-cache";
        context.Context.Response.Headers.Expires = "0";
    }
});

var repository = app.Services.GetRequiredService<IBlogRepository>();
await repository.InitializeAsync();

app.MapGet("/api/health", () => Results.Ok(new
{
    status = "ok",
    application = "AntiScam Blog API",
    storage = "SQLite",
    secondaryStorage = noSqlOptions.Enabled ? "MongoDB" : "disabled"
}));

app.MapGet("/api/storage", (BlogDatabaseOptions sqlite, NoSqlDatabaseOptions mongo) => Results.Ok(new
{
    primary = new { provider = "SQLite", path = sqlite.DatabasePath },
    secondary = new
    {
        provider = "MongoDB",
        enabled = mongo.Enabled,
        database = mongo.DatabaseName,
        collection = mongo.CollectionName
    }
}));

app.MapGet("/api/incidents", async (int? limit, IScamIncidentStore incidentStore, CancellationToken cancellationToken) =>
{
    var safeLimit = Math.Clamp(limit ?? 50, 1, 100);
    var incidents = await incidentStore.GetRecentAsync(safeLimit, cancellationToken);
    return Results.Ok(incidents);
});

app.MapGet("/api/workspace", (WorkspaceOptions options, BlogDatabaseOptions database) =>
{
    var directory = new DirectoryInfo(options.RootPath);
    return Results.Ok(new
    {
        rootPath = options.RootPath,
        exists = directory.Exists,
        databasePath = database.DatabasePath
    });
});

app.MapGet("/api/posts", async (IBlogRepository blogRepository) =>
{
    var posts = await blogRepository.GetAllAsync();
    return Results.Ok(posts);
});

app.MapGet("/api/posts/latest", async (IBlogRepository blogRepository) =>
{
    var post = await blogRepository.GetLatestAsync();
    return post is null ? Results.NotFound() : Results.Ok(post);
});

app.MapGet("/api/posts/{slug}", async (string slug, IBlogRepository blogRepository) =>
{
    var post = await blogRepository.GetBySlugAsync(slug);
    return post is null ? Results.NotFound() : Results.Ok(post);
});

app.MapGet("/api/posts/{postId:int}/comments", async (int postId, IBlogRepository blogRepository) =>
{
    var comments = await blogRepository.GetCommentsAsync(postId);
    return Results.Ok(comments);
});

app.MapPost("/api/posts/{postId:int}/comments", async (
    int postId,
    BlogCommentInput input,
    IBlogRepository blogRepository,
    IRiskAnalyzer riskAnalyzer,
    CancellationToken cancellationToken) =>
{
    var validation = BlogCommentValidator.Validate(input);
    if (validation.Count > 0)
    {
        return Results.ValidationProblem(validation);
    }

    // Use exactly the same analyzer as posts; comments simply have no title or summary.
    var risk = riskAnalyzer.Analyze(new BlogPostInput(string.Empty, string.Empty, input.Content, input.Author));
    if (!risk.CanPublish)
    {
        return Results.Json(new
        {
            message = "Comment was not published because scam risk was detected.",
            risk
        }, statusCode: StatusCodes.Status422UnprocessableEntity);
    }

    var created = await blogRepository.CreateCommentAsync(postId, input, cancellationToken);
    return created is null
        ? Results.NotFound()
        : Results.Created($"/api/posts/{postId}/comments/{created.Id}", created);
});

app.MapPost("/api/posts", async (
    BlogPostInput input,
    IBlogRepository blogRepository,
    IRiskAnalyzer riskAnalyzer,
    IBlockExplanationProvider blockExplanationProvider,
    IScamIncidentStore scamIncidentStore,
    CancellationToken cancellationToken) =>
{
    var validation = BlogPostValidator.Validate(input);
    if (validation.Count > 0)
    {
        return Results.ValidationProblem(validation);
    }

    var risk = riskAnalyzer.Analyze(input);
    if (!risk.CanPublish)
    {
        var aiExplanation = await blockExplanationProvider.ExplainAsync(input, risk, cancellationToken);
        await scamIncidentStore.RecordAsync(input, risk, cancellationToken);
        return Results.Json(new
        {
            message = "Post was not published because scam risk was detected.",
            aiExplanation,
            risk
        }, statusCode: StatusCodes.Status422UnprocessableEntity);
    }

    var created = await blogRepository.CreateAsync(input);
    return Results.Created($"/api/posts/{created.Slug}", created);
});

app.MapPut("/api/posts/{id:int}", async (
    int id,
    BlogPostInput input,
    IBlogRepository blogRepository,
    IRiskAnalyzer riskAnalyzer,
    IBlockExplanationProvider blockExplanationProvider,
    CancellationToken cancellationToken) =>
{
    var validation = BlogPostValidator.Validate(input);
    if (validation.Count > 0)
    {
        return Results.ValidationProblem(validation);
    }

    var risk = riskAnalyzer.Analyze(input);
    if (!risk.CanPublish)
    {
        var aiExplanation = await blockExplanationProvider.ExplainAsync(input, risk, cancellationToken);
        return Results.Json(new
        {
            message = "Post was not updated because scam risk was detected.",
            aiExplanation,
            risk
        }, statusCode: StatusCodes.Status422UnprocessableEntity);
    }

    var updated = await blogRepository.UpdateAsync(id, input);
    return updated is null ? Results.NotFound() : Results.Ok(updated);
});

app.Run();

public partial class Program
{
    private static string ResolvePath(string path, string basePath) =>
        Path.IsPathRooted(path) ? path : Path.GetFullPath(path, basePath);

}
