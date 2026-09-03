namespace AntiScam.Blog.Api.Models;

public static class BlogPostValidator
{
    public static Dictionary<string, string[]> Validate(BlogPostInput input)
    {
        var errors = new Dictionary<string, string[]>();

        AddIfEmpty(errors, nameof(input.Title), input.Title);
        AddIfEmpty(errors, nameof(input.Summary), input.Summary);
        AddIfEmpty(errors, nameof(input.Content), input.Content);
        AddIfEmpty(errors, nameof(input.Author), input.Author);

        if (!string.IsNullOrWhiteSpace(input.Title) && input.Title.Length > 160)
        {
            errors[nameof(input.Title)] = ["Title cannot be longer than 160 characters."];
        }

        if (!string.IsNullOrWhiteSpace(input.Summary) && input.Summary.Length > 300)
        {
            errors[nameof(input.Summary)] = ["Summary cannot be longer than 300 characters."];
        }

        return errors;
    }

    private static void AddIfEmpty(Dictionary<string, string[]> errors, string field, string? value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            errors[field] = [$"{field} is required."];
        }
    }
}
