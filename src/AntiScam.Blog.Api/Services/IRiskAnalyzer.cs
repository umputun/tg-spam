using AntiScam.Blog.Api.Models;

namespace AntiScam.Blog.Api.Services;

public interface IRiskAnalyzer
{
    RiskAssessment Analyze(BlogPostInput input);
}
