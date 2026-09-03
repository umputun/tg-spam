namespace AntiScam.Blog.Api;

public sealed record NetworkOptions
{
    public bool BindToLan { get; init; } = true;
    public int HttpPort { get; init; } = 5000;
}
