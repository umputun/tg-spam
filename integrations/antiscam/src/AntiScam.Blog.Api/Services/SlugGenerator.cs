using System.Globalization;
using System.Text;
using System.Text.RegularExpressions;

namespace AntiScam.Blog.Api.Services;

public sealed partial class SlugGenerator : ISlugGenerator
{
    public string Generate(string text) => CreateSlug(text);

    public static string CreateSlug(string text)
    {
        if (string.IsNullOrWhiteSpace(text))
        {
            return "post";
        }

        var normalized = text.Trim()
            .ToLowerInvariant()
            .Replace('ł', 'l')
            .Normalize(NormalizationForm.FormD);
        var builder = new StringBuilder(normalized.Length);

        foreach (var character in normalized)
        {
            var category = CharUnicodeInfo.GetUnicodeCategory(character);
            if (category != UnicodeCategory.NonSpacingMark)
            {
                builder.Append(character);
            }
        }

        var ascii = builder.ToString().Normalize(NormalizationForm.FormC);
        var slug = InvalidCharacters().Replace(ascii, "-");
        slug = DuplicateDashes().Replace(slug, "-").Trim('-');

        return string.IsNullOrWhiteSpace(slug) ? "post" : slug;
    }

    [GeneratedRegex("[^a-z0-9]+")]
    private static partial Regex InvalidCharacters();

    [GeneratedRegex("-{2,}")]
    private static partial Regex DuplicateDashes();
}
