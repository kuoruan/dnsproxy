package geosite

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLoader(t *testing.T) {
	// Use geosite.dat from the testdata directory.
	geositeFile := filepath.Join("testdata", "geosite.dat")

	// Ensure the file exists.
	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	t.Run("load_from_directory", func(t *testing.T) {
		// Create a temp directory and copy the file.
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "geosite.dat")

		data, err := os.ReadFile(geositeFile)
		require.NoError(t, err)

		err = os.WriteFile(tmpFile, data, 0644)
		require.NoError(t, err)

		// Load from a config directory path.
		loader, err := NewLoader(tmpDir, slog.Default())
		require.NoError(t, err)
		require.NotNil(t, loader)
		err = loader.Load(nil)
		require.NoError(t, err)
		assert.True(t, loader.IsLoaded())
		assert.Greater(t, len(loader.sites), 0)
	})

	t.Run("load_from_path", func(t *testing.T) {
		// Load from an absolute file path.
		loader, err := NewLoaderFromPath(geositeFile, slog.Default())
		require.NoError(t, err)
		require.NotNil(t, loader)
		err = loader.Load(nil)
		require.NoError(t, err)
		assert.True(t, loader.IsLoaded())
		assert.Greater(t, len(loader.sites), 0)
	})

	t.Run("file_not_found", func(t *testing.T) {
		// Handle a missing file gracefully.
		loader, err := NewLoaderFromPath("/nonexistent/path/geosite.dat", slog.Default())
		require.NoError(t, err) // Should not return an error
		require.NotNil(t, loader)
		err = loader.Load(nil)
		require.NoError(t, err)
		assert.False(t, loader.IsLoaded()) // Should remain not loaded
	})

	t.Run("invalid_file", func(t *testing.T) {
		// Create an invalid file.
		tmpDir := t.TempDir()
		invalidFile := filepath.Join(tmpDir, "invalid.dat")
		err := os.WriteFile(invalidFile, []byte("invalid data"), 0644)
		require.NoError(t, err)

		// Loading an invalid file should error.
		loader, err := NewLoaderFromPath(invalidFile, slog.Default())
		require.NoError(t, err)
		err = loader.Load(nil)
		assert.Error(t, err) // Should return an error
	})
}

func TestLoader_GetSite(t *testing.T) {
	geositeFile := filepath.Join("testdata", "geosite.dat")

	_, err := os.Stat(geositeFile)
	if os.IsNotExist(err) {
		t.Skip("geosite.dat not found in testdata, skipping test")
	}

	loader, err := NewLoaderFromPath(geositeFile, slog.Default())
	require.NoError(t, err)
	require.NotNil(t, loader)
	err = loader.Load(nil)
	require.NoError(t, err)

	testCases := []struct {
		name       string
		code       string
		shouldFind bool
	}{
		{
			name:       "google",
			code:       "google",
			shouldFind: true,
		},
		{
			name:       "cn",
			code:       "cn",
			shouldFind: true,
		},
		{
			name:       "geolocation-cn",
			code:       "geolocation-cn",
			shouldFind: true,
		},
		{
			name:       "nonexistent",
			code:       "nonexistent-code-12345",
			shouldFind: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			site, found := loader.GetSite(tc.code)
			assert.Equal(t, tc.shouldFind, found)
			if tc.shouldFind {
				assert.NotNil(t, site)
				assert.Equal(t, strings.ToUpper(tc.code), site.GetCountryCode())
				assert.Greater(t, len(site.GetDomain()), 0)
			} else {
				assert.Nil(t, site)
			}
		})
	}
}

func TestLoader_GetSites(t *testing.T) {
	loader, err := NewLoaderFromPath("testdata/geosite.dat", slog.Default())
	require.NoError(t, err)

	// Load multiple sites
	err = loader.Load([]string{"google", "cn", "geolocation-cn"})
	require.NoError(t, err)

	t.Run("get_all_loaded_sites", func(t *testing.T) {
		sites, notFound := loader.GetSites([]string{"google", "cn", "geolocation-cn"})

		assert.Equal(t, 3, len(sites), "should get 3 sites")
		assert.Empty(t, notFound, "should have no not-found codes")

		assert.Contains(t, sites, "google")
		assert.Contains(t, sites, "cn")
		assert.Contains(t, sites, "geolocation-cn")

		assert.NotNil(t, sites["google"])
		assert.NotNil(t, sites["cn"])
		assert.NotNil(t, sites["geolocation-cn"])
	})

	t.Run("get_some_loaded_some_not", func(t *testing.T) {
		sites, notFound := loader.GetSites([]string{"google", "nonexistent", "cn"})

		assert.Equal(t, 2, len(sites), "should get 2 sites")
		assert.Equal(t, 1, len(notFound), "should have 1 not-found code")

		assert.Contains(t, sites, "google")
		assert.Contains(t, sites, "cn")
		assert.Contains(t, notFound, "nonexistent")
	})

	t.Run("get_all_not_found", func(t *testing.T) {
		sites, notFound := loader.GetSites([]string{"nonexistent1", "nonexistent2"})

		assert.Empty(t, sites, "should have no sites")
		assert.Equal(t, 2, len(notFound), "should have 2 not-found codes")
		assert.Contains(t, notFound, "nonexistent1")
		assert.Contains(t, notFound, "nonexistent2")
	})

	t.Run("case_insensitive", func(t *testing.T) {
		sites, notFound := loader.GetSites([]string{"GOOGLE", "Cn", "GeoLocation-CN"})

		assert.Equal(t, 3, len(sites), "should get 3 sites (case insensitive)")
		assert.Empty(t, notFound, "should have no not-found codes")

		// Note: keys in returned map use the original case from input
		assert.Contains(t, sites, "GOOGLE")
		assert.Contains(t, sites, "Cn")
		assert.Contains(t, sites, "GeoLocation-CN")
	})

	t.Run("empty_input", func(t *testing.T) {
		sites, notFound := loader.GetSites([]string{})

		assert.Empty(t, sites, "should have no sites")
		assert.Empty(t, notFound, "should have no not-found codes")
	})

	t.Run("batch_vs_single", func(t *testing.T) {
		// Get using batch method
		batchSites, notFound := loader.GetSites([]string{"google", "cn"})
		assert.Equal(t, 2, len(batchSites))
		assert.Empty(t, notFound)

		// Get using single method and compare
		googleSite, ok := loader.GetSite("google")
		assert.True(t, ok)
		assert.Equal(t, googleSite, batchSites["google"], "batch and single should return same site")

		cnSite, ok := loader.GetSite("cn")
		assert.True(t, ok)
		assert.Equal(t, cnSite, batchSites["cn"], "batch and single should return same site")
	})
}

func TestLoader_GetSites_BeforeLoad(t *testing.T) {
	loader, err := NewLoaderFromPath("testdata/geosite.dat", slog.Default())
	require.NoError(t, err)

	// Try to get sites before loading
	sites, notFound := loader.GetSites([]string{"google", "cn"})

	assert.Empty(t, sites, "should have no sites before loading")
	assert.Equal(t, 2, len(notFound), "all codes should be not found")
	assert.Contains(t, notFound, "google")
	assert.Contains(t, notFound, "cn")
}

func TestLoader_IsLoaded(t *testing.T) {
	t.Run("loaded", func(t *testing.T) {
		geositeFile := filepath.Join("testdata", "geosite.dat")

		_, err := os.Stat(geositeFile)
		if os.IsNotExist(err) {
			t.Skip("geosite.dat not found in testdata, skipping test")
		}

		loader, err := NewLoaderFromPath(geositeFile, slog.Default())
		require.NoError(t, err)
		err = loader.Load(nil)
		require.NoError(t, err)
		assert.True(t, loader.IsLoaded())
	})

	t.Run("not_loaded", func(t *testing.T) {
		loader, err := NewLoaderFromPath("/nonexistent/geosite.dat", slog.Default())
		require.NoError(t, err)
		err = loader.Load(nil)
		require.NoError(t, err)
		assert.False(t, loader.IsLoaded())
	})
}
