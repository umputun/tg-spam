using System.Security.Cryptography;
using AntiScam.Blog.Api.Security;

namespace AntiScam.Blog.Api.Tests.Unit;

public sealed class AesGcmAuthenticatedEncryptorTests
{
    [Fact]
    public void EncryptDecrypt_RoundTripsPlainText()
    {
        var encryptor = new AesGcmAuthenticatedEncryptor(AesGcmAuthenticatedEncryptor.CreateKey());

        var payload = encryptor.Encrypt("Poufna notatka audytowa", "post:1");
        var plainText = encryptor.Decrypt(payload, "post:1");

        Assert.Equal("Poufna notatka audytowa", plainText);
        Assert.Equal(AesGcmAuthenticatedEncryptor.AlgorithmName, payload.Algorithm);
    }

    [Fact]
    public void Decrypt_RejectsChangedAdditionalData()
    {
        var encryptor = new AesGcmAuthenticatedEncryptor(AesGcmAuthenticatedEncryptor.CreateKey());
        var payload = encryptor.Encrypt("Poufna notatka audytowa", "post:1");

        Assert.ThrowsAny<CryptographicException>(() => encryptor.Decrypt(payload, "post:2"));
    }
}
