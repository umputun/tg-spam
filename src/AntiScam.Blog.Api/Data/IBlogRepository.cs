using AntiScam.Blog.Api.Models;

namespace AntiScam.Blog.Api.Data;

public interface IBlogRepository
{
    Task InitializeAsync(CancellationToken cancellationToken = default);

    Task<IReadOnlyList<BlogPost>> GetAllAsync(bool includeInactive = false, CancellationToken cancellationToken = default);

    Task<BlogPost?> GetLatestAsync(CancellationToken cancellationToken = default);

    Task<BlogPost?> GetBySlugAsync(string slug, CancellationToken cancellationToken = default);

    Task<IReadOnlyList<BlogComment>> GetCommentsAsync(int postId, CancellationToken cancellationToken = default);

    Task<BlogPost> CreateAsync(BlogPostInput input, CancellationToken cancellationToken = default);

    Task<BlogComment?> CreateCommentAsync(int postId, BlogCommentInput input, CancellationToken cancellationToken = default);

    Task<BlogPost?> UpdateAsync(int id, BlogPostInput input, CancellationToken cancellationToken = default);

    Task<bool> DeactivateAsync(int id, CancellationToken cancellationToken = default);

    Task<bool> RestoreAsync(int id, CancellationToken cancellationToken = default);
}
