package domain

// ScanSecrets scans text for secret patterns. Stub: implemented by WS-A.
func ScanSecrets(text string) []Finding { return nil }

// ScanMarkers scans text for classification markers. Stub.
func ScanMarkers(markers []string, text string) []Finding { return nil }

// CheckLinks checks links against an allowlist. Stub.
func CheckLinks(allow []string, text string) []Finding { return nil }
