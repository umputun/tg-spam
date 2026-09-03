using System.Text.RegularExpressions;
using AntiScam.Blog.Api.Models;
using Nager.PublicSuffix;
using Nager.PublicSuffix.RuleProviders;

namespace AntiScam.Blog.Api.Services;

public sealed partial class RiskAnalyzer : IRiskAnalyzer
{
    
    private static readonly IRuleProvider RuleProvider =
        new LocalFileRuleProvider("public_suffix_list.dat");

    private static readonly DomainParser DomainParser =
        new(RuleProvider);

    private static readonly HashSet<string> TrustedDomains = new(StringComparer.OrdinalIgnoreCase)
    {
        "google.com",
        "facebook.com",
        "messenger.com",
        "microsoft.com",
        "apple.com",
        "amazon.com",
        "paypal.com",
        "chatgpt.com"
    };

    private static readonly IReadOnlyDictionary<string, int> KeywordWeights = new Dictionary<string, int>
    {
        ["blik"] = 10,
        ["kod"] = 5,
        ["przelew"] = 7,
        ["pozycz"] = 8,
        ["pożycz"] = 8,
        ["szybko"] = 4,
        ["pilnie"] = 6,
        ["natychmiast"] = 6,
        ["wyslij"] = 5,
        ["wyślij"] = 5,
        ["potwierdz"] = 5,
        ["potwierdź"] = 5,
        ["bank"] = 3,
        ["konto"] = 3,
        ["zablokowane"] = 7,
        ["haslo"] = 5,
        ["hasło"] = 5
    };

    private static readonly string[] SafeSignals =
    [
        "dziekuje",
        "dziękuję",
        "spotkanie",
        "normalnie",
        "ok",
        "czesc",
        "cześć",
        "hej"
    ];

    private static readonly string[] IntentSignals =
    [
        "ostatnia szansa",
        "konto zablokowane",
        "natychmiast",
        "kliknij teraz",
        "potwierdz dane",
        "potwierdź dane"
    ];

    public RiskAssessment Analyze(BlogPostInput input)
    {
        var text = $"{input.Title} {input.Summary} {input.Content} {input.Author}";
        var normalizedText = DeobfuscateText(text);
        var textLow = normalizedText.ToLowerInvariant();
        var riskScore = 0;
        var reasons = new List<string>();

        var blikNumbers = BlikPattern().Matches(normalizedText)
            .Select(match => match.Value)
            .ToArray();
        if (blikNumbers.Length > 0)
        {
            var hasBlikContext = ContainsAny(textLow, ["blik", "kod", "przelew"]);
            riskScore += hasBlikContext ? 60 : 30;
            reasons.Add(hasBlikContext
                ? $"BLIK CONFIRMED: {string.Join(", ", blikNumbers)}"
                : $"BLIK UNCERTAIN: {string.Join(", ", blikNumbers)}");
        }

        var links = UrlPattern().Matches(text)
            .Select(match => match.Value)
            .ToArray();
        var safeLinks = new List<string>();
        var riskyLinks = new List<string>();
        var typosquattingLinks = new List<string>();

        foreach (var link in links)
        {
            var domain = ExtractDomain(link);
            if (IsTrustedDomain(domain))
            {
                safeLinks.Add(link);
            }
            else
            {
                riskyLinks.Add(link);
                if (IsTyposquattingDomain(domain))
                {
                    typosquattingLinks.Add(link);
                }
            }
        }

        if (riskyLinks.Count > 0)
        {
            riskScore += Math.Min(45, 20 + riskyLinks.Count * 10);
            reasons.Add($"Risky links: {string.Join(", ", riskyLinks)}");
        }

        if (typosquattingLinks.Count > 0)
        {
            riskScore = Math.Max(riskScore + 80, 90);
            reasons.Add($"Typosquatting links: {string.Join(", ", typosquattingLinks)}");
        }

        if (safeLinks.Count > 0)
        {
            riskScore -= Math.Min(15, safeLinks.Count * 5);
            reasons.Add($"Trusted links: {string.Join(", ", safeLinks)}");
        }

        var keywordScore = KeywordWeights.Sum(pair => Math.Min(CountOccurrences(textLow, pair.Key), 2) * pair.Value);
        keywordScore = Math.Min(keywordScore, 30);
        if (keywordScore > 0)
        {
            riskScore += keywordScore;
            reasons.Add($"Keyword score: {keywordScore}");
        }

        var safeSignalHits = SafeSignals.Count(signal => textLow.Contains(signal, StringComparison.Ordinal));
        if (safeSignalHits >= 2)
        {
            riskScore -= 10;
            reasons.Add("Low-risk context detected");
        }

        if (IntentSignals.Any(signal => textLow.Contains(signal, StringComparison.Ordinal)))
        {
            riskScore += 10;
            reasons.Add("Scam intent detected");
        }

        riskScore = Math.Clamp(riskScore, 0, 100);
        var status = riskScore >= 80
            ? RiskStatuses.HighRisk
            : riskScore >= 50
                ? RiskStatuses.MediumRisk
                : RiskStatuses.LowRisk;

        return new RiskAssessment(status, riskScore, reasons, safeLinks, riskyLinks);
    }

    private static string ExtractDomain(string url)
    {
        if (!Uri.TryCreate(url, UriKind.Absolute, out var parsed))
        {
            return string.Empty;
        }

        try
        {
            var domainInfo = DomainParser.Parse(parsed.Host);

            return domainInfo.RegistrableDomain?
                .ToLowerInvariant()
                ?? FallbackRegisteredDomain(parsed.Host);
        }
        catch
        {
            return FallbackRegisteredDomain(parsed.Host);
        }
    }

    private static string FallbackRegisteredDomain(string host)
    {
        var labels = host.Split('.', StringSplitOptions.RemoveEmptyEntries);
        return labels.Length < 2
            ? string.Empty
            : string.Join('.', labels[^2..]).ToLowerInvariant();
    }

    private static bool IsTrustedDomain(string domain)
    {
        return TrustedDomains.Contains(domain);
    }

    private static bool IsTyposquattingDomain(string domain)
    {
        return !string.IsNullOrWhiteSpace(domain)
            && !IsTrustedDomain(domain)
            && TrustedDomains.Any(trusted => LevenshteinDistance(domain, trusted) <= 2);
    }

    private static int LevenshteinDistance(string left, string right)
    {
        if (left == right)
        {
            return 0;
        }

        if (left.Length == 0)
        {
            return right.Length;
        }

        if (right.Length == 0)
        {
            return left.Length;
        }

        var previous = Enumerable.Range(0, right.Length + 1).ToArray();

        for (var leftIndex = 1; leftIndex <= left.Length; leftIndex++)
        {
            var current = new int[right.Length + 1];
            current[0] = leftIndex;

            for (var rightIndex = 1; rightIndex <= right.Length; rightIndex++)
            {
                var substitutionCost = left[leftIndex - 1] == right[rightIndex - 1] ? 0 : 1;
                current[rightIndex] = Math.Min(
                    Math.Min(previous[rightIndex] + 1, current[rightIndex - 1] + 1),
                    previous[rightIndex - 1] + substitutionCost);
            }

            previous = current;
        }

        return previous[^1];
    }

    private static string DeobfuscateText(string text)
    {
        var normalized = SingleLetterChainPattern().Replace(text, match =>
            string.Concat(match.Value.Where(char.IsLetter)));
        normalized = InWordSpecialPattern().Replace(normalized, string.Empty);
        normalized = WhitespacePattern().Replace(normalized, " ");
        return normalized.Trim();
    }

    private static bool ContainsAny(string text, IEnumerable<string> values)
    {
        return values.Any(value => text.Contains(value, StringComparison.Ordinal));
    }

    private static int CountOccurrences(string text, string value)
    {
        var count = 0;
        var index = 0;

        while ((index = text.IndexOf(value, index, StringComparison.Ordinal)) >= 0)
        {
            count++;
            index += value.Length;
        }

        return count;
    }

    [GeneratedRegex(@"\b\d{6}\b")]
    private static partial Regex BlikPattern();

    [GeneratedRegex(@"https?://(?:[-\w.]|(?:%[\da-fA-F]{2}))+[^\s]*")]
    private static partial Regex UrlPattern();

    [GeneratedRegex(@"(?i)(?<!\w)(?:[a-z]\W+){2,}[a-z](?!\w)")]
    private static partial Regex SingleLetterChainPattern();

    [GeneratedRegex(@"(?<=[^\W_])(?:[^\w\s]|_)+(?=[^\W_])")]
    private static partial Regex InWordSpecialPattern();

    [GeneratedRegex(@"\s+")]
    private static partial Regex WhitespacePattern();
}
