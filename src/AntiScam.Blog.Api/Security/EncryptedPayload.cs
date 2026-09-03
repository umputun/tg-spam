namespace AntiScam.Blog.Api.Security;

public sealed record EncryptedPayload(
    string Algorithm,
    string Nonce,
    string Tag,
    string CipherText);
