namespace AntiScam.Blog.Api;

public sealed record HttpsOptions
{
    public bool Enabled { get; init; }
    public string ListenAddress { get; init; } = "0.0.0.0";
    public int Port { get; init; } = 5001;
    public string CertificatePath { get; init; } = "certs/antiscam-server.pfx";
    public string? CertificatePassword { get; init; }
}
