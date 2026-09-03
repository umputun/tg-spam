using Microsoft.AspNetCore.Mvc.Testing;
using Microsoft.AspNetCore.Hosting;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.DependencyInjection.Extensions;
using AntiScam.Blog.Api.Data;
using AntiScam.Blog.Api.Models;
using AntiScam.Blog.Api.Services;

namespace AntiScam.Blog.Api.Tests.Integration;

public sealed class BlogApiFactory : WebApplicationFactory<Program>, IDisposable
{
    public string DatabasePath { get; } = Path.Combine(
        Path.GetTempPath(),
        "antiscam-blog-tests",
        $"{Guid.NewGuid():N}.sqlite");

    protected override void ConfigureWebHost(IWebHostBuilder builder)
    {
        Environment.SetEnvironmentVariable("ANTISCAM_BLOG_DB", DatabasePath);
        builder.ConfigureServices(services =>
        {
            services.RemoveAll<IBlockExplanationProvider>();
            services.AddSingleton<IBlockExplanationProvider, TestBlockExplanationProvider>();
            services.RemoveAll<IScamIncidentStore>();
            services.AddSingleton<IScamIncidentStore, NullScamIncidentStore>();
        });
    }

    protected override void Dispose(bool disposing)
    {
        base.Dispose(disposing);
        Environment.SetEnvironmentVariable("ANTISCAM_BLOG_DB", null);

        if (File.Exists(DatabasePath))
        {
            File.Delete(DatabasePath);
        }
    }

    private sealed class TestBlockExplanationProvider : IBlockExplanationProvider
    {
        public Task<string> ExplainAsync(BlogPostInput input, RiskAssessment risk, CancellationToken cancellationToken) =>
            Task.FromResult($"AI z ai.py wyjasnia blokade. Sygnały: {string.Join("; ", risk.Reasons)}.");
    }
}
