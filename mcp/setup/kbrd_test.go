package setup

import "testing"

func TestKBRDConfigure_EnablesGlobalMCP(t *testing.T) {
	isolateHome(t)

	res, err := configureKBRD("127.0.0.1:9999", false)
	if err != nil {
		t.Fatalf("configureKBRD: %v", err)
	}
	if res.Status != StatusEnabled {
		t.Fatalf("result = %+v, want enabled", res)
	}
	assertFileContains(t, userConfigPath(t), "[mcp]\naddr = \"127.0.0.1:9999\"\nenabled = true\n")
}

func TestKBRDConfigure_PreservesUnrelatedConfig(t *testing.T) {
	isolateHome(t)
	writeTestFile(t, userConfigPath(t), "# keep\n[display]\ncolumn_width = 40\n")

	if _, err := configureKBRD("127.0.0.1:7777", false); err != nil {
		t.Fatalf("configureKBRD: %v", err)
	}
	assertFileContains(t, userConfigPath(t), "# keep\n[display]\ncolumn_width = 40\n")
	assertFileContains(t, userConfigPath(t), "[mcp]\naddr = \"127.0.0.1:7777\"\nenabled = true\n")
}
