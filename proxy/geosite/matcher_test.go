package geosite

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMatcher(t *testing.T) {
	geositeFile := filepath.Join("testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	loader, err := NewLoaderFromPath(geositeFile, slog.Default())
	require.NoError(t, err)
	err = loader.Load(nil)
	require.NoError(t, err)

	t.Run("create_matcher", func(t *testing.T) {
		site, ok := loader.GetSite("google")
		require.True(t, ok)

		matcher := NewMatcher(site, "google", false)
		assert.NotNil(t, matcher)
	})

	t.Run("create_matcher_with_negation", func(t *testing.T) {
		site, ok := loader.GetSite("cn")
		require.True(t, ok)

		matcher := NewMatcher(site, "geolocation-!cn", true)
		assert.NotNil(t, matcher)
	})
}

func TestMatcher_Match(t *testing.T) {
	geositeFile := filepath.Join("testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	loader, err := NewLoaderFromPath(geositeFile, slog.Default())
	require.NoError(t, err)
	err = loader.Load(nil)
	require.NoError(t, err)

	t.Run("google_domains", func(t *testing.T) {
		site, ok := loader.GetSite("google")
		require.True(t, ok)

		matcher := NewMatcher(site, "google", false)

		testCases := []struct {
			domain      string
			shouldMatch bool
		}{
			{"www.google.com", true},
			{"google.com", true},
			{"mail.google.com", true},
			{"www.youtube.com", true},
			{"youtube.com", true},
			{"www.baidu.com", false},
			{"example.com", false},
		}

		for _, tc := range testCases {
			t.Run(tc.domain, func(t *testing.T) {
				result := matcher.Match(tc.domain)
				assert.Equal(t, tc.shouldMatch, result, "domain: %s", tc.domain)
			})
		}
	})

	t.Run("cn_domains", func(t *testing.T) {
		site, ok := loader.GetSite("cn")
		require.True(t, ok)

		matcher := NewMatcher(site, "cn", false)

		testCases := []struct {
			domain      string
			shouldMatch bool
		}{
			{"baidu.com", true},
			{"www.baidu.com", true},
			{"taobao.com", true},
			{"qq.com", true},
			{"google.com", false},
			{"facebook.com", false},
		}

		for _, tc := range testCases {
			t.Run(tc.domain, func(t *testing.T) {
				result := matcher.Match(tc.domain)
				assert.Equal(t, tc.shouldMatch, result, "domain: %s", tc.domain)
			})
		}
	})

	t.Run("negation_cn", func(t *testing.T) {
		site, ok := loader.GetSite("cn")
		require.True(t, ok)

		// Create a negation matcher.
		matcher := NewMatcher(site, "geolocation-!cn", true)

		testCases := []struct {
			domain      string
			shouldMatch bool
		}{
			{"google.com", true},  // Non-China domains should match
			{"facebook.com", true},
			{"twitter.com", true},
			{"baidu.com", false}, // China domains should not match
			{"taobao.com", false},
			{"qq.com", false},
		}

		for _, tc := range testCases {
			t.Run(tc.domain, func(t *testing.T) {
				result := matcher.Match(tc.domain)
				assert.Equal(t, tc.shouldMatch, result, "domain: %s (negated)", tc.domain)
			})
		}
	})

	t.Run("domain_normalization", func(t *testing.T) {
		site, ok := loader.GetSite("google")
		require.True(t, ok)

		matcher := NewMatcher(site, "google", false)

		testCases := []struct {
			name   string
			domain string
		}{
			{"with_trailing_dot", "google.com."},
			{"without_trailing_dot", "google.com"},
			{"uppercase", "GOOGLE.COM"},
			{"mixed_case", "Google.Com"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// All normalized formats should match.
				result := matcher.Match(tc.domain)
				assert.True(t, result, "domain: %s", tc.domain)
			})
		}
	})
}

func TestMatcher_MatchTypes(t *testing.T) {
	geositeFile := filepath.Join("testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	loader, err := NewLoaderFromPath(geositeFile, slog.Default())
	require.NoError(t, err)
	err = loader.Load(nil)
	require.NoError(t, err)

	// Test multiple match types; actual types depend on geosite.dat content.
	t.Run("various_match_types", func(t *testing.T) {
		// Google site includes multiple match types.
		site, ok := loader.GetSite("google")
		require.True(t, ok)

		matcher := NewMatcher(site, "google", false)

		// Subdomain matches
		assert.True(t, matcher.Match("mail.google.com"))
		assert.True(t, matcher.Match("drive.google.com"))
		assert.True(t, matcher.Match("www.youtube.com"))

		// Non-matching domains
		assert.False(t, matcher.Match("microsoft.com"))
		assert.False(t, matcher.Match("notgoogle.com"))
	})
}

func BenchmarkMatcher_Match(b *testing.B) {
	geositeFile := filepath.Join("testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		b.Skip("geosite.dat not found in testdata, skipping benchmark")
	}

	loader, err := NewLoaderFromPath(geositeFile, slog.Default())
	require.NoError(b, err)
	err = loader.Load(nil)
	require.NoError(b, err)

	site, ok := loader.GetSite("google")
	require.True(b, ok)

	matcher := NewMatcher(site, "google", false)

	domains := []string{
		"www.google.com",
		"mail.google.com",
		"www.youtube.com",
		"www.baidu.com",
		"example.com",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = matcher.Match(domains[i%len(domains)])
	}
}
