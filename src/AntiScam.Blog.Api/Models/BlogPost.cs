namespace AntiScam.Blog.Api.Models;

public sealed record BlogPost(
    int Id,
    string Title,
    string Slug,
    string Summary,
    string Content,
    string Author,
    DateTimeOffset PublishedAt,
    DateTimeOffset UpdatedAt,
    bool IsActive);
