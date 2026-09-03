using MongoDB.Bson;
using MongoDB.Bson.Serialization.Attributes;

namespace AntiScam.Blog.Api.Data;

public sealed class ScamIncident
{
    [BsonId]
    [BsonRepresentation(BsonType.ObjectId)]
    public string Id { get; init; } = string.Empty;

    public string Title { get; init; } = string.Empty;
    public string Summary { get; init; } = string.Empty;
    public string Content { get; init; } = string.Empty;
    public string Author { get; init; } = string.Empty;
    public string Status { get; init; } = string.Empty;
    public int RiskScore { get; init; }
    public string[] Reasons { get; init; } = [];
    public string[] RiskyLinks { get; init; } = [];
    public DateTime RecordedAtUtc { get; init; }
}
