namespace AntiScam.Blog.Api.Models;

public sealed record BlogComment(
    int Id,
    int PostId,
    string Content,
    string Author,
    DateTimeOffset PublishedAt);
