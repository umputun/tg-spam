using AntiScam.Blog.Api.Models;

namespace AntiScam.Blog.Api.Tests.Unit;

public sealed class BlogPostValidatorTests
{
    [Fact]
    public void Validate_ReturnsErrorsForMissingFields()
    {
        var input = new BlogPostInput("", "", "", "");

        var errors = BlogPostValidator.Validate(input);

        Assert.Contains(nameof(BlogPostInput.Title), errors.Keys);
        Assert.Contains(nameof(BlogPostInput.Summary), errors.Keys);
        Assert.Contains(nameof(BlogPostInput.Content), errors.Keys);
        Assert.Contains(nameof(BlogPostInput.Author), errors.Keys);
    }

    [Fact]
    public void Validate_ReturnsNoErrorsForCompletePost()
    {
        var input = new BlogPostInput("Tytuł", "Opis", "Treść", "Autor");

        var errors = BlogPostValidator.Validate(input);

        Assert.Empty(errors);
    }
}
