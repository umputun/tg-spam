namespace AntiScam.Blog.Api.Models;

public static class BlogCommentValidator
{
    public static Dictionary<string, string[]> Validate(BlogCommentInput input)
    {
        var errors = new Dictionary<string, string[]>();

        if (string.IsNullOrWhiteSpace(input.Content))
        {
            errors[nameof(input.Content)] = ["Content is required."];
        }
        else if (input.Content.Length > 2_000)
        {
            errors[nameof(input.Content)] = ["Content cannot be longer than 2000 characters."];
        }

        if (string.IsNullOrWhiteSpace(input.Author))
        {
            errors[nameof(input.Author)] = ["Author is required."];
        }
        else if (input.Author.Length > 100)
        {
            errors[nameof(input.Author)] = ["Author cannot be longer than 100 characters."];
        }

        return errors;
    }
}
