namespace AntiScam.Blog.Api.Models;

public sealed record BlogPostInput(
    string Title,
    string Summary,
    string Content,
    string Author);
