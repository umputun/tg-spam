namespace AntiScam.Blog.Api.Data;

/// <summary>Configuration of the optional MongoDB incident store.</summary>
public sealed record NoSqlDatabaseOptions
{
    public bool Enabled { get; init; }

    public string ConnectionString { get; init; } = "mongodb://localhost:27017";

    public string DatabaseName { get; init; } = "antiscam";

    public string CollectionName { get; init; } = "blocked-submissions";
}
