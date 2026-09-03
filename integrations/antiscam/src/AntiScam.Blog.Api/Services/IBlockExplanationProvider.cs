using AntiScam.Blog.Api.Models;

namespace AntiScam.Blog.Api.Services;

public interface IBlockExplanationProvider
{
    Task<string> ExplainAsync(BlogPostInput input, RiskAssessment risk, CancellationToken cancellationToken);
}
