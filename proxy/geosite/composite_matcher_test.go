package geosite

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompositeMatcher(t *testing.T) {
	loader, err := NewLoaderFromPath("testdata/geosite.dat", slog.Default())
	require.NoError(t, err)

	// Load google and cn sites
	err = loader.Load([]string{"google", "cn"})
	require.NoError(t, err)

	googleSite, ok := loader.GetSite("google")
	require.True(t, ok)
	cnSite, ok := loader.GetSite("cn")
	require.True(t, ok)

	// Create individual matchers
	googleMatcher := NewMatcher(googleSite, "google", false)
	cnMatcher := NewMatcher(cnSite, "cn", false)

	// Create composite matcher
	compositeMatcher := NewCompositeMatcher([]*Matcher{googleMatcher, cnMatcher})

	testCases := []struct {
		domain      string
		shouldMatch bool
		description string
	}{
		{"google.com", true, "should match google domain"},
		{"www.google.com", true, "should match google subdomain"},
		{"youtube.com", true, "should match youtube (google-owned)"},
		{"baidu.com", true, "should match baidu (cn)"},
		{"qq.com", true, "should match qq (cn)"},
		{"taobao.com", true, "should match taobao (cn)"},
		{"example.com", false, "should not match unrelated domain"},
		{"facebook.com", false, "should not match facebook"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := compositeMatcher.Match(tc.domain)
			assert.Equal(t, tc.shouldMatch, result, tc.description)
		})
	}
}

func TestCompositeMatcher_Codes(t *testing.T) {
	loader, err := NewLoaderFromPath("testdata/geosite.dat", slog.Default())
	require.NoError(t, err)

	err = loader.Load([]string{"google", "cn"})
	require.NoError(t, err)

	googleSite, _ := loader.GetSite("google")
	cnSite, _ := loader.GetSite("cn")

	googleMatcher := NewMatcher(googleSite, "google", false)
	cnMatcher := NewMatcher(cnSite, "cn", false)

	compositeMatcher := NewCompositeMatcher([]*Matcher{googleMatcher, cnMatcher})

	codes := compositeMatcher.Codes()
	assert.Equal(t, 2, len(codes))
	assert.Contains(t, codes, "google")
	assert.Contains(t, codes, "cn")
}

func TestCompositeMatcher_WithNegation(t *testing.T) {
	loader, err := NewLoaderFromPath("testdata/geosite.dat", slog.Default())
	require.NoError(t, err)

	err = loader.Load([]string{"google", "geolocation-cn"})
	require.NoError(t, err)

	googleSite, ok := loader.GetSite("google")
	require.True(t, ok)
	cnSite, ok := loader.GetSite("geolocation-cn")
	require.True(t, ok)

	// Create composite matcher with google and negated cn
	googleMatcher := NewMatcher(googleSite, "google", false)
	notCnMatcher := NewMatcher(cnSite, "geolocation-!cn", true)

	compositeMatcher := NewCompositeMatcher([]*Matcher{googleMatcher, notCnMatcher})

	testCases := []struct {
		domain      string
		shouldMatch bool
		description string
	}{
		{"google.com", true, "should match google domain"},
		{"facebook.com", true, "should match non-cn domain (negation)"},
		{"example.com", true, "should match non-cn domain (negation)"},
		{"baidu.com", false, "should not match cn domain (because of negation)"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := compositeMatcher.Match(tc.domain)
			assert.Equal(t, tc.shouldMatch, result, tc.description)
		})
	}
}

func TestCompositeMatcher_Empty(t *testing.T) {
	// Create composite matcher with no matchers
	compositeMatcher := NewCompositeMatcher([]*Matcher{})

	// Should not match anything
	assert.False(t, compositeMatcher.Match("google.com"))
	assert.False(t, compositeMatcher.Match("example.com"))
	assert.Empty(t, compositeMatcher.Codes())
}
