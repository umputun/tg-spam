using AntiScam.Blog.Api.Models;

namespace AntiScam.Blog.Api.Data;

/// <summary>Used while the optional MongoDB integration is disabled.</summary>
public sealed class NullScamIncidentStore : IScamIncidentStore
{
    public Task RecordAsync(BlogPostInput input, RiskAssessment risk, CancellationToken cancellationToken = default) =>
        Task.CompletedTask;

    public Task<IReadOnlyList<ScamIncident>> GetRecentAsync(int limit, CancellationToken cancellationToken = default) =>
        Task.FromResult<IReadOnlyList<ScamIncident>>([]);
}
