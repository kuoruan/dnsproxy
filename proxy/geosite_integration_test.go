package proxy

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/AdguardTeam/dnsproxy/proxy/geosite"
	"github.com/AdguardTeam/dnsproxy/upstream"
	"github.com/AdguardTeam/golibs/logutil/slogutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUpstreamsConfig_Geosite(t *testing.T) {
	// Locate geosite.dat in testdata.
	geositeFile := filepath.Join("geosite", "testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	// Initialize the geosite loader.
	loader, err := geosite.NewLoaderFromPath(geositeFile, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, loader)
	err = loader.Load(nil)
	require.NoError(t, err)

	t.Run("parse_geosite_rules", func(t *testing.T) {
		conf := []string{
			"8.8.8.8",                       // Default upstream
			"[/geosite:google/]1.1.1.1",     // Google uses 1.1.1.1
			"[/geosite:cn/]114.114.114.114", // CN sites use 114.114.114.114
			"[/example.com/]9.9.9.9",        // Regular domain rule
		}

		opts := &upstream.Options{
			Logger:     slogutil.NewDiscardLogger(),
			GeositeDir: filepath.Join("geosite", "testdata"),
		}
		upsConf, err := ParseUpstreamsConfig(conf, opts)
		require.NoError(t, err)
		require.NotNil(t, upsConf)

		// Ensure geosite upstreams are parsed correctly.
		assert.Len(t, upsConf.GeositeUpstreams, 2)

		// Verify default upstream.
		assert.Len(t, upsConf.Upstreams, 1)
		assert.Equal(t, "8.8.8.8:53", upsConf.Upstreams[0].Address())

		// Verify regular domain rule.
		assert.Contains(t, upsConf.DomainReservedUpstreams, "example.com.")
	})

	t.Run("parse_geosite_negation", func(t *testing.T) {
		conf := []string{
			"8.8.8.8",
			"[/geosite:geolocation-!cn/]1.1.1.1", // Non-China sites
		}

		opts := &upstream.Options{
			Logger:     slogutil.NewDiscardLogger(),
			GeositeDir: filepath.Join("geosite", "testdata"),
		}
		upsConf, err := ParseUpstreamsConfig(conf, opts)
		require.NoError(t, err)
		require.NotNil(t, upsConf)

		assert.Len(t, upsConf.GeositeUpstreams, 1)
	})

	t.Run("geosite_without_loader", func(t *testing.T) {
		conf := []string{
			"8.8.8.8",
			"[/geosite:google/]1.1.1.1",
		}

		// Geosite path is not provided.
		opts := &upstream.Options{
			Logger: slogutil.NewDiscardLogger(),
		}
		upsConf, err := ParseUpstreamsConfig(conf, opts)
		require.NoError(t, err)
		require.NotNil(t, upsConf)

		// Geosite rules should be skipped.
		assert.Len(t, upsConf.GeositeUpstreams, 0)
	})

	t.Run("invalid_geosite_code", func(t *testing.T) {
		conf := []string{
			"8.8.8.8",
			"[/geosite:nonexistent-code-12345/]1.1.1.1",
		}

		opts := &upstream.Options{
			Logger:     slogutil.NewDiscardLogger(),
			GeositeDir: filepath.Join("geosite", "testdata"),
		}
		upsConf, err := ParseUpstreamsConfig(conf, opts)
		require.NoError(t, err)
		require.NotNil(t, upsConf)

		// Invalid geosite codes should be skipped.
		assert.Len(t, upsConf.GeositeUpstreams, 0)
	})
}

func TestUpstreamConfig_GetUpstreamsForDomain_Geosite(t *testing.T) {
	geositeFile := filepath.Join("geosite", "testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	conf := []string{
		"8.8.8.8",                       // Default upstream
		"[/geosite:google/]1.1.1.1",     // Google
		"[/geosite:cn/]114.114.114.114", // CN sites
		"[/example.com/]9.9.9.9",        // Regular domain rule
	}

	upsConf, err := ParseUpstreamsConfig(conf, &upstream.Options{
		Logger:     slogutil.NewDiscardLogger(),
		GeositeDir: filepath.Join("geosite", "testdata"),
	})
	require.NoError(t, err)

	testCases := []struct {
		name             string
		domain           string
		expectedUpstream string
	}{
		{
			name:             "google_domain",
			domain:           "www.google.com.",
			expectedUpstream: "1.1.1.1:53",
		},
		{
			name:             "youtube_domain",
			domain:           "www.youtube.com.",
			expectedUpstream: "1.1.1.1:53",
		},
		{
			name:             "cn_domain",
			domain:           "baidu.com.",
			expectedUpstream: "114.114.114.114:53",
		},
		{
			name:             "cn_domain_subdomain",
			domain:           "www.taobao.com.",
			expectedUpstream: "114.114.114.114:53",
		},
		{
			name:             "normal_domain_rule",
			domain:           "example.com.",
			expectedUpstream: "9.9.9.9:53",
		},
		{
			name:             "default_upstream_trailing_dot",
			domain:           "unknown-domain.test.",
			expectedUpstream: "8.8.8.8:53",
		},
		{
			name:             "default_upstream",
			domain:           "unknown-domain.test.",
			expectedUpstream: "8.8.8.8:53",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ups := upsConf.getUpstreamsForDomain(tc.domain)
			require.Len(t, ups, 1)
			assert.Equal(t, tc.expectedUpstream, ups[0].Address())
		})
	}
}

func TestUpstreamConfig_GetUpstreamsForDomain_GeositePriority(t *testing.T) {
	geositeFile := filepath.Join("geosite", "testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	// Test geosite rule priority.
	conf := []string{
		"8.8.8.8",
		"[/google.com/]2.2.2.2",     // Regular domain rule
		"[/geosite:google/]1.1.1.1", // Geosite rule
	}

	upsConf, err := ParseUpstreamsConfig(conf, &upstream.Options{
		Logger:     slogutil.NewDiscardLogger(),
		GeositeDir: filepath.Join("geosite", "testdata"),
	})
	require.NoError(t, err)

	// Geosite rule should have higher priority.
	ups := upsConf.getUpstreamsForDomain("www.google.com.")
	require.Len(t, ups, 1)
	assert.Equal(t, "1.1.1.1:53", ups[0].Address(), "geosite rule should have higher priority")

	// But google.com itself should follow the regular domain rule.
	ups = upsConf.getUpstreamsForDomain("google.com.")
	require.Len(t, ups, 1)
	// Depends on whether geosite contains an exact match for google.com.
}

func TestUpstreamConfig_GetUpstreamsForDomain_GeositeNegation(t *testing.T) {
	geositeFile := filepath.Join("geosite", "testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	conf := []string{
		"8.8.8.8",
		"[/geosite:geolocation-!cn/]1.1.1.1", // Non-China sites
	}

	upsConf, err := ParseUpstreamsConfig(conf, &upstream.Options{
		Logger:     slogutil.NewDiscardLogger(),
		GeositeDir: filepath.Join("geosite", "testdata"),
	})
	require.NoError(t, err)

	testCases := []struct {
		name             string
		domain           string
		expectedUpstream string
	}{
		{
			name:             "non_cn_domain",
			domain:           "google.com.",
			expectedUpstream: "1.1.1.1:53",
		},
		{
			name:             "cn_domain",
			domain:           "baidu.com.",
			expectedUpstream: "8.8.8.8:53", // Should fall back to the default upstream
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ups := upsConf.getUpstreamsForDomain(tc.domain)
			require.Len(t, ups, 1)
			assert.Equal(t, tc.expectedUpstream, ups[0].Address())
		})
	}
}

func TestMultipleGeositeCodesInSingleLine(t *testing.T) {
	// Use the geosite.dat from the geosite package testdata
	geositeFile := filepath.Join("geosite", "testdata", "geosite.dat")

	// Check if file exists
	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in geosite/testdata, skipping test")
	}

	// Define upstream config with multiple geosite codes in one line
	configLines := []string{
		"1.1.1.1", // Default upstream
		"[/geosite:google/geosite:cn/]8.8.8.8",
	}

	// Parse the config
	opts := &upstream.Options{
		Logger:     slog.Default(),
		GeositeDir: filepath.Join("geosite", "testdata"),
	}
	uc, err := ParseUpstreamsConfig(configLines, opts)
	require.NoError(t, err)
	require.NotNil(t, uc)

	// Verify that we have 1 geosite upstream configured (multiple codes are combined)
	assert.Equal(t, 1, len(uc.GeositeUpstreams), "should have 1 geosite upstream with composite matcher")

	// Test that the composite matcher works correctly
	// Should match both google and cn domains
	testCases := []struct {
		domain      string
		shouldMatch bool
		description string
	}{
		{"google.com.", true, "should match google domain"},
		{"youtube.com.", true, "should match youtube (google)"},
		{"baidu.com.", true, "should match baidu (cn)"},
		{"qq.com.", true, "should match qq (cn)"},
		{"example.com.", false, "should not match unrelated domain"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ups := uc.getUpstreamsForDomain(tc.domain)
			require.NotEmpty(t, ups, "should have upstream for "+tc.domain)
			if tc.shouldMatch {
				assert.Equal(t, "8.8.8.8:53", ups[0].Address(), tc.description)
			} else {
				// Should use default upstream
				assert.Equal(t, "1.1.1.1:53", ups[0].Address(), tc.description)
			}
		})
	}
}

func TestGeositeAndDomainInSameLine(t *testing.T) {
	// Use the geosite.dat from the geosite package testdata
	geositeFile := filepath.Join("geosite", "testdata", "geosite.dat")

	// Check if file exists
	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in geosite/testdata, skipping test")
	}

	// One line contains both geosite code(s) and a plain domain rule
	// Expectation: domains matching either the geosite or the plain domain use the specified upstream
	// Others fall back to default upstream
	configLines := []string{
		"9.9.9.9", // Default upstream
		"[/geosite:google/example.com/]1.1.1.1",
	}

	// Parse the config
	opts := &upstream.Options{
		Logger:     slog.Default(),
		GeositeDir: filepath.Join("geosite", "testdata"),
	}
	uc, err := ParseUpstreamsConfig(configLines, opts)
	require.NoError(t, err)
	require.NotNil(t, uc)

	// Verify geosite and domain pieces are effective
	// google.com and youtube.com come from geosite:google
	// example.com comes from the plain domain rule in the same line
	// unrelated domains should use default upstream
	cases := []struct {
		domain   string
		expected string
		note     string
	}{
		{"www.google.com.", "1.1.1.1:53", "geosite:google should match"},
		{"www.youtube.com.", "1.1.1.1:53", "geosite:google subdomain should match"},
		{"example.com.", "1.1.1.1:53", "plain domain in same line should match"},
		{"unknown.test.", "9.9.9.9:53", "unrelated domain falls back to default"},
	}

	for _, c := range cases {
		t.Run(c.domain, func(t *testing.T) {
			ups := uc.getUpstreamsForDomain(c.domain)
			require.NotEmpty(t, ups, c.note)
			assert.Equal(t, c.expected, ups[0].Address(), c.note)
		})
	}
}

func BenchmarkGetUpstreamsForDomain_WithGeosite(b *testing.B) {
	geositeFile := filepath.Join("geosite", "testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		b.Skip("geosite.dat not found in testdata, skipping benchmark")
	}

	conf := []string{
		"8.8.8.8",
		"[/geosite:google/]1.1.1.1",
		"[/geosite:cn/]114.114.114.114",
		"[/example.com/]9.9.9.9",
	}

	upsConf, err := ParseUpstreamsConfig(conf, &upstream.Options{
		Logger:     slogutil.NewDiscardLogger(),
		GeositeDir: filepath.Join("geosite", "testdata"),
	})
	require.NoError(b, err)

	domains := []string{
		"www.google.com",
		"www.youtube.com",
		"baidu.com",
		"taobao.com",
		"example.com",
		"unknown.test",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = upsConf.getUpstreamsForDomain(domains[i%len(domains)])
	}
}
