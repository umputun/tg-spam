namespace AntiScam.Blog.Api.Models;

public sealed record RiskAssessment(
    string Status,
    int RiskScore,
    IReadOnlyList<string> Reasons,
    IReadOnlyList<string> SafeLinks,
    IReadOnlyList<string> RiskyLinks)
{
    public bool CanPublish => Status == RiskStatuses.LowRisk;

    public string BlockExplanation
    {
        get
        {
            if (CanPublish)
            {
                return $"Publikacja nie zostala zablokowana, bo /scan ocenil wpis jako {Status} ({RiskScore}/100).";
            }

            var signals = Reasons.Count > 0
                ? string.Join("; ", Reasons)
                : "wynik ryzyka przekroczyl prog blokady";

            return $"Publikacja zostala zablokowana, bo /scan ocenil wpis jako {Status} ({RiskScore}/100). Sygnaly: {signals}.";
        }
    }
}
