using AntiScam.Blog.Api.Models;
using AntiScam.Blog.Api.Services;
using Microsoft.Data.Sqlite;

namespace AntiScam.Blog.Api.Data;

public sealed class SqliteBlogRepository(BlogDatabaseOptions options, ISlugGenerator slugGenerator) : IBlogRepository
{
    public async Task InitializeAsync(CancellationToken cancellationToken = default)
    {
        var directory = Path.GetDirectoryName(options.DatabasePath);
        if (!string.IsNullOrWhiteSpace(directory))
        {
            Directory.CreateDirectory(directory);
        }

        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        await using (var command = connection.CreateCommand())
        {
            command.CommandText = """
                CREATE TABLE IF NOT EXISTS Posts (
                    Id INTEGER PRIMARY KEY AUTOINCREMENT,
                    Title TEXT NOT NULL,
                    Slug TEXT NOT NULL UNIQUE,
                    Summary TEXT NOT NULL,
                    Content TEXT NOT NULL,
                    Author TEXT NOT NULL,
                    PublishedAt TEXT NOT NULL,
                    UpdatedAt TEXT NOT NULL,
                    IsActive INTEGER NOT NULL DEFAULT 1
                );

                CREATE TABLE IF NOT EXISTS Comments (
                    Id INTEGER PRIMARY KEY AUTOINCREMENT,
                    PostId INTEGER NOT NULL,
                    Content TEXT NOT NULL,
                    Author TEXT NOT NULL,
                    PublishedAt TEXT NOT NULL,
                    FOREIGN KEY (PostId) REFERENCES Posts(Id) ON DELETE CASCADE
                );

                CREATE INDEX IF NOT EXISTS IX_Comments_PostId_PublishedAt
                ON Comments (PostId, PublishedAt, Id);

                """;
            await command.ExecuteNonQueryAsync(cancellationToken);
        }

        await using (var migration = connection.CreateCommand())
        {
            migration.CommandText = "ALTER TABLE Posts ADD COLUMN IsActive INTEGER NOT NULL DEFAULT 1;";
            try { await migration.ExecuteNonQueryAsync(cancellationToken); }
            catch (SqliteException exception) when (exception.SqliteErrorCode == 1 && exception.Message.Contains("duplicate column name", StringComparison.OrdinalIgnoreCase)) { }
        }

        await using (var removeAuthTables = connection.CreateCommand())
        {
            removeAuthTables.CommandText = "DROP TABLE IF EXISTS UserSessions; DROP TABLE IF EXISTS Users;";
            await removeAuthTables.ExecuteNonQueryAsync(cancellationToken);
        }

        await SeedAsync(connection, cancellationToken);
    }

    public async Task<IReadOnlyList<BlogPost>> GetAllAsync(bool includeInactive = false, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        await using var command = connection.CreateCommand();
        command.CommandText = """
            SELECT Id, Title, Slug, Summary, Content, Author, PublishedAt, UpdatedAt, IsActive
            FROM Posts
            WHERE IsActive = 1 OR $includeInactive = 1
            ORDER BY PublishedAt DESC, Id DESC;
            """;
        command.Parameters.AddWithValue("$includeInactive", includeInactive ? 1 : 0);

        var posts = new List<BlogPost>();
        await using var reader = await command.ExecuteReaderAsync(cancellationToken);
        while (await reader.ReadAsync(cancellationToken))
        {
            posts.Add(ReadPost(reader));
        }

        return posts;
    }

    public async Task<BlogPost?> GetLatestAsync(CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        await using var command = connection.CreateCommand();
        command.CommandText = """
            SELECT Id, Title, Slug, Summary, Content, Author, PublishedAt, UpdatedAt, IsActive
            FROM Posts
            WHERE IsActive = 1
            ORDER BY PublishedAt DESC, Id DESC
            LIMIT 1;
            """;

        await using var reader = await command.ExecuteReaderAsync(cancellationToken);
        return await reader.ReadAsync(cancellationToken) ? ReadPost(reader) : null;
    }

    public async Task<BlogPost?> GetBySlugAsync(string slug, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        await using var command = connection.CreateCommand();
        command.CommandText = """
            SELECT Id, Title, Slug, Summary, Content, Author, PublishedAt, UpdatedAt, IsActive
            FROM Posts
            WHERE Slug = $slug AND IsActive = 1;
            """;
        command.Parameters.AddWithValue("$slug", slug);

        await using var reader = await command.ExecuteReaderAsync(cancellationToken);
        return await reader.ReadAsync(cancellationToken) ? ReadPost(reader) : null;
    }

    public async Task<IReadOnlyList<BlogComment>> GetCommentsAsync(int postId, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        await using var command = connection.CreateCommand();
        command.CommandText = """
            SELECT Id, PostId, Content, Author, PublishedAt
            FROM Comments
            WHERE PostId = $postId
            ORDER BY PublishedAt ASC, Id ASC;
            """;
        command.Parameters.AddWithValue("$postId", postId);

        var comments = new List<BlogComment>();
        await using var reader = await command.ExecuteReaderAsync(cancellationToken);
        while (await reader.ReadAsync(cancellationToken))
        {
            comments.Add(ReadComment(reader));
        }

        return comments;
    }

    public async Task<BlogPost> CreateAsync(BlogPostInput input, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        var now = DateTimeOffset.UtcNow;
        var slug = await CreateUniqueSlugAsync(connection, input.Title, null, cancellationToken);

        await using var command = connection.CreateCommand();
        command.CommandText = """
            INSERT INTO Posts (Title, Slug, Summary, Content, Author, PublishedAt, UpdatedAt, IsActive)
            VALUES ($title, $slug, $summary, $content, $author, $publishedAt, $updatedAt, 1)
            RETURNING Id, Title, Slug, Summary, Content, Author, PublishedAt, UpdatedAt, IsActive;
            """;
        AddPostParameters(command, input, slug, now, now);

        await using var reader = await command.ExecuteReaderAsync(cancellationToken);
        if (!await reader.ReadAsync(cancellationToken))
        {
            throw new InvalidOperationException("Post creation did not return a row.");
        }

        return ReadPost(reader);
    }

    public async Task<BlogComment?> CreateCommentAsync(int postId, BlogCommentInput input, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        if (!await ExistsAsync(connection, postId, cancellationToken))
        {
            return null;
        }

        var now = DateTimeOffset.UtcNow;
        await using var command = connection.CreateCommand();
        command.CommandText = """
            INSERT INTO Comments (PostId, Content, Author, PublishedAt)
            VALUES ($postId, $content, $author, $publishedAt)
            RETURNING Id, PostId, Content, Author, PublishedAt;
            """;
        command.Parameters.AddWithValue("$postId", postId);
        command.Parameters.AddWithValue("$content", input.Content.Trim());
        command.Parameters.AddWithValue("$author", input.Author.Trim());
        command.Parameters.AddWithValue("$publishedAt", now.ToString("O"));

        await using var reader = await command.ExecuteReaderAsync(cancellationToken);
        return await reader.ReadAsync(cancellationToken) ? ReadComment(reader) : null;
    }

    public async Task<BlogPost?> UpdateAsync(int id, BlogPostInput input, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        if (!await ExistsAsync(connection, id, cancellationToken))
        {
            return null;
        }

        var current = DateTimeOffset.UtcNow;
        var slug = await CreateUniqueSlugAsync(connection, input.Title, id, cancellationToken);

        await using var command = connection.CreateCommand();
        command.CommandText = """
            UPDATE Posts
            SET Title = $title,
                Slug = $slug,
                Summary = $summary,
                Content = $content,
                Author = $author,
                UpdatedAt = $updatedAt
            WHERE Id = $id
            RETURNING Id, Title, Slug, Summary, Content, Author, PublishedAt, UpdatedAt, IsActive;
            """;
        command.Parameters.AddWithValue("$id", id);
        AddPostParameters(command, input, slug, null, current);

        await using var reader = await command.ExecuteReaderAsync(cancellationToken);
        return await reader.ReadAsync(cancellationToken) ? ReadPost(reader) : null;
    }

    public async Task<bool> DeactivateAsync(int id, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);

        await using var command = connection.CreateCommand();
        command.CommandText = "UPDATE Posts SET IsActive = 0, UpdatedAt = $updatedAt WHERE Id = $id AND IsActive = 1;";
        command.Parameters.AddWithValue("$id", id);
        command.Parameters.AddWithValue("$updatedAt", DateTimeOffset.UtcNow.ToString("O"));

        return await command.ExecuteNonQueryAsync(cancellationToken) > 0;
    }

    public async Task<bool> RestoreAsync(int id, CancellationToken cancellationToken = default)
    {
        await using var connection = CreateConnection();
        await connection.OpenAsync(cancellationToken);
        await using var command = connection.CreateCommand();
        command.CommandText = "UPDATE Posts SET IsActive = 1, UpdatedAt = $updatedAt WHERE Id = $id AND IsActive = 0;";
        command.Parameters.AddWithValue("$id", id);
        command.Parameters.AddWithValue("$updatedAt", DateTimeOffset.UtcNow.ToString("O"));
        return await command.ExecuteNonQueryAsync(cancellationToken) > 0;
    }

    private SqliteConnection CreateConnection()
    {
        var builder = new SqliteConnectionStringBuilder
        {
            DataSource = options.DatabasePath,
            Pooling = false,
            ForeignKeys = true
        };

        return new SqliteConnection(builder.ConnectionString);
    }

    private static async Task SeedAsync(SqliteConnection connection, CancellationToken cancellationToken)
    {
        await using var countCommand = connection.CreateCommand();
        countCommand.CommandText = "SELECT COUNT(*) FROM Posts;";
        var count = (long)(await countCommand.ExecuteScalarAsync(cancellationToken) ?? 0L);
        if (count > 0)
        {
            return;
        }

        var now = DateTimeOffset.UtcNow;
        var seedPosts = new[]
        {
            new BlogPostInput(
                "Jak rozpoznać fałszywą wiadomość BLIK",
                "Krótki przewodnik po sygnałach ostrzegawczych w wiadomościach proszących o kod BLIK.",
                "Nie podawaj kodu BLIK osobie, która naciska na szybkie działanie. Najpierw zadzwoń do znajomego innym kanałem i potwierdź prośbę.",
                "AntiScam Team"),
            new BlogPostInput(
                "Checklist bezpieczeństwa linków",
                "Lista szybkich kontroli przed kliknięciem w link z SMS-a, maila lub komunikatora.",
                "Sprawdź domenę, certyfikat, literówki i kontekst. Banki oraz urzędy nie powinny wymuszać pilnych logowań przez skrócone linki.",
                "AntiScam Team")
        };

        foreach (var post in seedPosts)
        {
            await using var insert = connection.CreateCommand();
            insert.CommandText = """
                INSERT INTO Posts (Title, Slug, Summary, Content, Author, PublishedAt, UpdatedAt)
                VALUES ($title, $slug, $summary, $content, $author, $publishedAt, $updatedAt);
                """;
            insert.Parameters.AddWithValue("$title", post.Title);
            insert.Parameters.AddWithValue("$slug", SlugGenerator.CreateSlug(post.Title));
            insert.Parameters.AddWithValue("$summary", post.Summary);
            insert.Parameters.AddWithValue("$content", post.Content);
            insert.Parameters.AddWithValue("$author", post.Author);
            insert.Parameters.AddWithValue("$publishedAt", now.ToString("O"));
            insert.Parameters.AddWithValue("$updatedAt", now.ToString("O"));
            await insert.ExecuteNonQueryAsync(cancellationToken);
        }
    }

    private async Task<string> CreateUniqueSlugAsync(
        SqliteConnection connection,
        string title,
        int? currentPostId,
        CancellationToken cancellationToken)
    {
        var baseSlug = slugGenerator.Generate(title);
        var slug = baseSlug;
        var suffix = 2;

        while (await SlugExistsAsync(connection, slug, currentPostId, cancellationToken))
        {
            slug = $"{baseSlug}-{suffix}";
            suffix++;
        }

        return slug;
    }

    private static async Task<bool> SlugExistsAsync(
        SqliteConnection connection,
        string slug,
        int? currentPostId,
        CancellationToken cancellationToken)
    {
        await using var command = connection.CreateCommand();
        command.CommandText = currentPostId is null
            ? "SELECT COUNT(*) FROM Posts WHERE Slug = $slug;"
            : "SELECT COUNT(*) FROM Posts WHERE Slug = $slug AND Id <> $id;";
        command.Parameters.AddWithValue("$slug", slug);
        if (currentPostId is not null)
        {
            command.Parameters.AddWithValue("$id", currentPostId.Value);
        }

        var count = (long)(await command.ExecuteScalarAsync(cancellationToken) ?? 0L);
        return count > 0;
    }

    private static async Task<bool> ExistsAsync(SqliteConnection connection, int id, CancellationToken cancellationToken)
    {
        await using var command = connection.CreateCommand();
        command.CommandText = "SELECT COUNT(*) FROM Posts WHERE Id = $id;";
        command.Parameters.AddWithValue("$id", id);
        var count = (long)(await command.ExecuteScalarAsync(cancellationToken) ?? 0L);
        return count > 0;
    }

    private static void AddPostParameters(
        SqliteCommand command,
        BlogPostInput input,
        string slug,
        DateTimeOffset? publishedAt,
        DateTimeOffset updatedAt)
    {
        command.Parameters.AddWithValue("$title", input.Title.Trim());
        command.Parameters.AddWithValue("$slug", slug);
        command.Parameters.AddWithValue("$summary", input.Summary.Trim());
        command.Parameters.AddWithValue("$content", input.Content.Trim());
        command.Parameters.AddWithValue("$author", input.Author.Trim());
        command.Parameters.AddWithValue("$updatedAt", updatedAt.ToString("O"));
        if (publishedAt is not null)
        {
            command.Parameters.AddWithValue("$publishedAt", publishedAt.Value.ToString("O"));
        }
    }

    private static BlogPost ReadPost(SqliteDataReader reader)
    {
        return new BlogPost(
            reader.GetInt32(0),
            reader.GetString(1),
            reader.GetString(2),
            reader.GetString(3),
            reader.GetString(4),
            reader.GetString(5),
            DateTimeOffset.Parse(reader.GetString(6)),
            DateTimeOffset.Parse(reader.GetString(7)),
            reader.GetInt64(8) == 1);
    }

    private static BlogComment ReadComment(SqliteDataReader reader)
    {
        return new BlogComment(
            reader.GetInt32(0),
            reader.GetInt32(1),
            reader.GetString(2),
            reader.GetString(3),
            DateTimeOffset.Parse(reader.GetString(4)));
    }
}
