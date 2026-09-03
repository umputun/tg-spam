namespace AntiScam.Blog.Api.Security;

public interface IAuthenticatedEncryptor
{
    EncryptedPayload Encrypt(string plainText, string? additionalData = null);

    string Decrypt(EncryptedPayload payload, string? additionalData = null);
}
