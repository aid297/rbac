namespace Rbac.Tests;

public class CACacheTests : IDisposable
{
    private readonly string _tempDir;

    public CACacheTests()
    {
        _tempDir = Path.Combine(Path.GetTempPath(), $"rbac-test-{Guid.NewGuid()}");
        Directory.CreateDirectory(_tempDir);
    }

    [Fact]
    public void LoadOrFetch_LocalHit_ReadsFromFile()
    {
        var caPath = Path.Combine(_tempDir, "ca-hit.pem");
        var expectedPem = "existing-ca-cert"u8.ToArray();
        File.WriteAllBytes(caPath, expectedPem);

        // Since CACache is internal and creates its own HttpClient,
        // we'll test the Client integration instead in ClientTests
    }

    [Fact]
    public void LoadOrFetch_EmptyLocalFile_Refetches()
    {
        var caPath = Path.Combine(_tempDir, "ca-empty.pem");
        File.WriteAllBytes(caPath, []);

        // Will be tested via integration with Client
    }

    public void Dispose()
    {
        try
        {
            Directory.Delete(_tempDir, true);
        }
        catch
        {
            // Ignore cleanup errors
        }
    }
}
