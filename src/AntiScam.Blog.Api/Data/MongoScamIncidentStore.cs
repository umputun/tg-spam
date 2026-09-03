using AntiScam.Blog.Api.Models;
using MongoDB.Bson;
using MongoDB.Driver;

namespace AntiScam.Blog.Api.Data;

/// <summary>
/// Persists rejected submissions in MongoDB. Failures are logged so that an optional
/// audit store never changes the HTTP result or the SQLite blog workflow.
/// </summary>
public sealed class MongoScamIncidentStore : IScamIncidentStore
{
    private readonly IMongoCollection<ScamIncident> _incidents;
    private readonly ILogger<MongoScamIncidentStore> _logger;

    public MongoScamIncidentStore(NoSqlDatabaseOptions options, ILogger<MongoScamIncidentStore> logger)
    {
        var client = new MongoClient(options.ConnectionString);
        _incidents = client
            .GetDatabase(options.DatabaseName)
            .GetCollection<ScamIncident>(options.CollectionName);
        _logger = logger;
    }

    public async Task RecordAsync(BlogPostInput input, RiskAssessment risk, CancellationToken cancellationToken = default)
    {
        var incident = new ScamIncident
        {
            Id = ObjectId.GenerateNewId().ToString(),
            Title = input.Title.Trim(),
            Summary = input.Summary.Trim(),
            Content = input.Content.Trim(),
            Author = input.Author.Trim(),
            Status = risk.Status,
            RiskScore = risk.RiskScore,
            Reasons = risk.Reasons.ToArray(),
            RiskyLinks = risk.RiskyLinks.ToArray(),
            RecordedAtUtc = DateTime.UtcNow
        };

        try
        {
            await _incidents.InsertOneAsync(incident, cancellationToken: cancellationToken);
        }
        catch (Exception exception) when (exception is MongoException or TimeoutException)
        {
            _logger.LogWarning(exception, "Could not save rejected submission to the optional MongoDB store.");
        }
    }

    public async Task<IReadOnlyList<ScamIncident>> GetRecentAsync(int limit, CancellationToken cancellationToken = default)
    {
        try
        {
            return await _incidents
                .Find(FilterDefinition<ScamIncident>.Empty)
                .SortByDescending(incident => incident.RecordedAtUtc)
                .Limit(limit)
                .ToListAsync(cancellationToken);
        }
        catch (Exception exception) when (exception is MongoException or TimeoutException)
        {
            _logger.LogWarning(exception, "Could not read incidents from the optional MongoDB store.");
            return [];
        }
    }
}
