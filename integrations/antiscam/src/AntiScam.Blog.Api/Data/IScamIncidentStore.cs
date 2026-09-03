using AntiScam.Blog.Api.Models;

namespace AntiScam.Blog.Api.Data;

/// <summary>Secondary store for submissions rejected by the scam-risk analysis.</summary>
public interface IScamIncidentStore
{
    Task RecordAsync(BlogPostInput input, RiskAssessment risk, CancellationToken cancellationToken = default);

    Task<IReadOnlyList<ScamIncident>> GetRecentAsync(int limit, CancellationToken cancellationToken = default);
}
