using System.Security.Cryptography;
using System.Text;

namespace AntiScam.Blog.Api.Security;

public sealed class AesGcmAuthenticatedEncryptor : IAuthenticatedEncryptor
{
    public const string AlgorithmName = "AES-GCM-256";

    private const int NonceSize = 12;
    private const int TagSize = 16;
    private const int KeySize = 32;

    private readonly byte[] _key;

    public AesGcmAuthenticatedEncryptor(byte[] key)
    {
        if (key.Length != KeySize)
        {
            throw new ArgumentException("AES-GCM-256 requires a 32-byte key.", nameof(key));
        }

        _key = key.ToArray();
    }

    public EncryptedPayload Encrypt(string plainText, string? additionalData = null)
    {
        ArgumentNullException.ThrowIfNull(plainText);

        var nonce = RandomNumberGenerator.GetBytes(NonceSize);
        var plainBytes = Encoding.UTF8.GetBytes(plainText);
        var cipherText = new byte[plainBytes.Length];
        var tag = new byte[TagSize];

        using var aes = new AesGcm(_key, TagSize);
        aes.Encrypt(nonce, plainBytes, cipherText, tag, EncodeAdditionalData(additionalData));

        return new EncryptedPayload(
            AlgorithmName,
            Convert.ToBase64String(nonce),
            Convert.ToBase64String(tag),
            Convert.ToBase64String(cipherText));
    }

    public string Decrypt(EncryptedPayload payload, string? additionalData = null)
    {
        if (payload.Algorithm != AlgorithmName)
        {
            throw new CryptographicException("Unsupported encryption algorithm.");
        }

        var nonce = Convert.FromBase64String(payload.Nonce);
        var tag = Convert.FromBase64String(payload.Tag);
        var cipherText = Convert.FromBase64String(payload.CipherText);
        var plainBytes = new byte[cipherText.Length];

        using var aes = new AesGcm(_key, TagSize);
        aes.Decrypt(nonce, cipherText, tag, plainBytes, EncodeAdditionalData(additionalData));

        return Encoding.UTF8.GetString(plainBytes);
    }

    public static byte[] CreateKey() => RandomNumberGenerator.GetBytes(KeySize);

    private static byte[]? EncodeAdditionalData(string? additionalData)
    {
        return additionalData is null ? null : Encoding.UTF8.GetBytes(additionalData);
    }
}
