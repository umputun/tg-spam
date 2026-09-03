using AntiScam.Blog.Api.Services;

namespace AntiScam.Blog.Api.Tests.Unit;

public sealed class SlugGeneratorTests
{
    [Theory]
    [InlineData("Jak rozpoznać fałszywą wiadomość BLIK", "jak-rozpoznac-falszywa-wiadomosc-blik")]
    [InlineData("  Linki, banki i SMS-y!!! ", "linki-banki-i-sms-y")]
    [InlineData("", "post")]
    public void Generate_ReturnsUrlFriendlySlug(string title, string expected)
    {
        var generator = new SlugGenerator();

        var slug = generator.Generate(title);

        Assert.Equal(expected, slug);
    }
}
