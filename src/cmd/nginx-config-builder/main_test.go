package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetCurrentConfigVersionDirectory(t *testing.T) {
	// Test case 1: Directory with multiple release directories
	t.Run("MultipleReleaseDirectories", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create test release directories with date/sequence format
		releaseDirs := []string{
			"release-20011225.1", // 2001/12/25, sequence 1
			"release-20011225.2", // 2001/12/25, sequence 2
			"release-20011225.3", // 2001/12/25, sequence 3 (latest)
		}

		for _, dir := range releaseDirs {
			err := os.MkdirAll(filepath.Join(tempDir, dir), 0755)
			if err != nil {
				t.Fatalf("Failed to create test directory %s: %v", dir, err)
			}
		}

		// Create a non-release directory to ensure it's ignored
		err := os.MkdirAll(filepath.Join(tempDir, "not-a-release"), 0755)
		if err != nil {
			t.Fatalf("Failed to create non-release directory: %v", err)
		}

		// Test the function
		result, err := getCurrentConfigVersionDirectory(tempDir)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Should return the latest directory (highest sequence)
		expected := filepath.Join(tempDir, "release-20011225.3")
		if result != expected {
			t.Errorf("Expected %s, got: %s", expected, result)
		}
	})

	// Test case 2: Directory with no release directories
	t.Run("NoReleaseDirectories", func(t *testing.T) {
		emptyDir := t.TempDir()

		result, err := getCurrentConfigVersionDirectory(emptyDir)

		if err != nil {
			t.Errorf("Expected no error for empty directory, got: %v", err)
		}

		// Should return a new release directory path with today's date
		expectedDate := time.Now().Format("20060102")
		expectedPath := filepath.Join(emptyDir, fmt.Sprintf("release-%s.1", expectedDate))
		if result != expectedPath {
			t.Errorf("Expected %s, got: %s", expectedPath, result)
		}
	})

	// Test case 3: Non-existent directory
	t.Run("NonExistentDirectory", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")

		result, err := getCurrentConfigVersionDirectory(nonExistentDir)

		// The function should return a new release directory path even for non-existent directories
		if err != nil {
			t.Errorf("Expected no error for non-existent directory, got: %v", err)
		}

		// Should return a new release directory path with today's date
		expectedDate := time.Now().Format("20060102")
		expectedPath := filepath.Join(nonExistentDir, fmt.Sprintf("release-%s.1", expectedDate))
		if result != expectedPath {
			t.Errorf("Expected %s, got: %s", expectedPath, result)
		}
	})

	// Test case 4: Directory with mixed files and directories
	t.Run("MixedFilesAndDirectories", func(t *testing.T) {
		mixedDir := t.TempDir()

		// Create release directories with proper format
		releaseDirs := []string{
			"release-20011225.1",
			"release-20011225.2",
		}

		for _, dir := range releaseDirs {
			err := os.MkdirAll(filepath.Join(mixedDir, dir), 0755)
			if err != nil {
				t.Fatalf("Failed to create test directory %s: %v", dir, err)
			}
		}

		// Create some files that should be ignored
		files := []string{
			"config.conf",
			"release-file.txt", // This should be ignored (not a directory)
		}

		for _, file := range files {
			err := os.WriteFile(filepath.Join(mixedDir, file), []byte("test"), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file %s: %v", file, err)
			}
		}

		result, err := getCurrentConfigVersionDirectory(mixedDir)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Should return the latest directory (highest sequence)
		expected := filepath.Join(mixedDir, "release-20011225.2")
		if result != expected {
			t.Errorf("Expected %s, got: %s", expected, result)
		}
	})

	// Test case 5: Test date comparison (different dates)
	t.Run("DifferentDates", func(t *testing.T) {
		dateDir := t.TempDir()

		// Create release directories with different dates
		releaseDirs := []string{
			"release-20011224.5",  // 2001/12/24, sequence 5
			"release-20011225.1",  // 2001/12/25, sequence 1 (latest date)
			"release-20011223.10", // 2001/12/23, sequence 10
		}

		for _, dir := range releaseDirs {
			err := os.MkdirAll(filepath.Join(dateDir, dir), 0755)
			if err != nil {
				t.Fatalf("Failed to create test directory %s: %v", dir, err)
			}
		}

		result, err := getCurrentConfigVersionDirectory(dateDir)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Should return the latest date (20011225.1)
		expected := filepath.Join(dateDir, "release-20011225.1")
		if result != expected {
			t.Errorf("Expected %s, got: %s", expected, result)
		}
	})

	// Test case 6: Test invalid format directories (should be ignored)
	t.Run("InvalidFormatDirectories", func(t *testing.T) {
		invalidDir := t.TempDir()

		// Create directories with invalid formats
		invalidDirs := []string{
			"release-1.0.0",         // Wrong format
			"release-2.0.0-beta",    // Wrong format
			"release-3.0.0-alpha.1", // Wrong format
			"release-20011225",      // Missing sequence
			"release-20011225.",     // Missing sequence number
		}

		for _, dir := range invalidDirs {
			err := os.MkdirAll(filepath.Join(invalidDir, dir), 0755)
			if err != nil {
				t.Fatalf("Failed to create test directory %s: %v", dir, err)
			}
		}

		result, err := getCurrentConfigVersionDirectory(invalidDir)

		// Should return error since no valid directories found
		if err == nil {
			t.Errorf("Expected error for invalid format directories, got: %s", result)
		}

		if result != "" {
			t.Errorf("Expected empty string, got: %s", result)
		}
	})
}

// TestGetCurrentConfigVersionDirectoryComplex tests complex scenarios
func TestGetCurrentConfigVersionDirectoryComplex(t *testing.T) {
	// Test case: Complex scenario with multiple dates and sequences
	t.Run("ComplexScenario", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create test release directories with various dates and sequences
		releaseDirs := []string{
			"release-20011224.1",  // 24/12/01, sequence 1
			"release-20011224.3",  // 24/12/01, sequence 3
			"release-20011225.1",  // 25/12/01, sequence 1 (latest date)
			"release-20011225.2",  // 25/12/01, sequence 2 (latest overall)
			"release-20011223.10", // 23/12/01, sequence 10 (older date)
		}

		for _, dir := range releaseDirs {
			err := os.MkdirAll(filepath.Join(tempDir, dir), 0755)
			if err != nil {
				t.Fatalf("Failed to create test directory %s: %v", dir, err)
			}
		}

		result, err := getCurrentConfigVersionDirectory(tempDir)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Should return the latest directory (251201.2)
		expected := filepath.Join(tempDir, "release-20011225.2")
		if result != expected {
			t.Errorf("Expected %s, got: %s", expected, result)
		}

		// Verify that the directories exist
		for _, dir := range releaseDirs {
			fullPath := filepath.Join(tempDir, dir)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				t.Errorf("Expected directory %s to exist", fullPath)
			}
		}
	})
}

// TestDeploymentFunctions tests the deployment-related functions
func TestDeploymentFunctions(t *testing.T) {
	// Test getPreviousVersionDirectory
	t.Run("GetPreviousVersionDirectory", func(t *testing.T) {
		tempDir := t.TempDir()

		// Test case 1: No current symlink exists
		prevDir, err := getPreviousVersionDirectory(tempDir)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		if prevDir != "" {
			t.Errorf("Expected empty string, got: %s", prevDir)
		}

		// Test case 2: Create a current symlink
		releaseDir := filepath.Join(tempDir, "release-20011225.1")
		if err := os.MkdirAll(releaseDir, 0755); err != nil {
			t.Fatalf("Failed to create release directory: %v", err)
		}

		currentSymlink := filepath.Join(tempDir, "current")
		if err := os.Symlink("release-20011225.1", currentSymlink); err != nil {
			t.Fatalf("Failed to create symlink: %v", err)
		}

		prevDir, err = getPreviousVersionDirectory(tempDir)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}
		expected := filepath.Join(tempDir, "release-20011225.1")
		if prevDir != expected {
			t.Errorf("Expected %s, got: %s", expected, prevDir)
		}
	})

	// Test copyConfigToRelease
	t.Run("CopyConfigToRelease", func(t *testing.T) {
		tempDir := t.TempDir()
		releaseDir := filepath.Join(tempDir, "release-20011225.1")
		configContent := "test config content"
		filename := "test.conf"

		err := copyConfigToRelease(configContent, releaseDir, filename)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Verify the file was created
		configPath := filepath.Join(releaseDir, filename)
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Errorf("Expected config file to exist at %s", configPath)
		}

		// Verify the content
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Errorf("Failed to read config file: %v", err)
		}
		if string(content) != configContent {
			t.Errorf("Expected content %s, got: %s", configContent, string(content))
		}
	})

	// Test copyConfigToRelease with nested directories
	t.Run("CopyConfigToReleaseWithNestedDirs", func(t *testing.T) {
		tempDir := t.TempDir()
		releaseDir := filepath.Join(tempDir, "release-20011225.1")
		configContent := "vhost config content"
		filename := "vhosts/example.com/vhost.conf"

		err := copyConfigToRelease(configContent, releaseDir, filename)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Verify the nested directory was created
		vhostDir := filepath.Join(releaseDir, "vhosts", "example.com")
		if _, err := os.Stat(vhostDir); os.IsNotExist(err) {
			t.Errorf("Expected vhost directory to exist at %s", vhostDir)
		}

		// Verify the file was created
		configPath := filepath.Join(releaseDir, filename)
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Errorf("Expected config file to exist at %s", configPath)
		}

		// Verify the content
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Errorf("Failed to read config file: %v", err)
		}
		if string(content) != configContent {
			t.Errorf("Expected content %s, got: %s", configContent, string(content))
		}
	})

	// Test updateCurrentSymlink
	t.Run("UpdateCurrentSymlink", func(t *testing.T) {
		tempDir := t.TempDir()
		releaseDir := filepath.Join(tempDir, "release-20011225.1")

		// Create the release directory
		if err := os.MkdirAll(releaseDir, 0755); err != nil {
			t.Fatalf("Failed to create release directory: %v", err)
		}

		err := updateCurrentSymlink(tempDir, releaseDir)
		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Verify the symlink was created
		currentSymlink := filepath.Join(tempDir, "current")
		if _, err := os.Lstat(currentSymlink); os.IsNotExist(err) {
			t.Errorf("Expected current symlink to exist")
		}

		// Verify the symlink points to the correct directory
		target, err := os.Readlink(currentSymlink)
		if err != nil {
			t.Errorf("Failed to read symlink: %v", err)
		}
		expected := "release-20011225.1"
		if target != expected {
			t.Errorf("Expected symlink to point to %s, got: %s", expected, target)
		}
	})
}

// TestGetCurrentConfigVersionDirectoryNewRelease tests the new behavior when no releases exist
func TestGetCurrentConfigVersionDirectoryNewRelease(t *testing.T) {
	t.Run("CreatesNewReleaseWhenNoneExist", func(t *testing.T) {
		tempDir := t.TempDir()

		result, err := getCurrentConfigVersionDirectory(tempDir)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Should return a new release directory path with today's date
		expectedDate := time.Now().Format("20060102")
		expectedPath := filepath.Join(tempDir, fmt.Sprintf("release-%s.1", expectedDate))
		if result != expectedPath {
			t.Errorf("Expected %s, got: %s", expectedPath, result)
		}

		// Verify the directory doesn't exist yet (it's just the path, not created)
		if _, err := os.Stat(result); !os.IsNotExist(err) {
			t.Errorf("Expected directory %s to not exist yet, but it does", result)
		}
	})

	t.Run("CreatesNewReleaseForNonExistentPath", func(t *testing.T) {
		tempDir := t.TempDir()
		nonExistentDir := filepath.Join(tempDir, "does-not-exist")

		result, err := getCurrentConfigVersionDirectory(nonExistentDir)

		if err != nil {
			t.Errorf("Expected no error, got: %v", err)
		}

		// Should return a new release directory path with today's date
		expectedDate := time.Now().Format("20060102")
		expectedPath := filepath.Join(nonExistentDir, fmt.Sprintf("release-%s.1", expectedDate))
		if result != expectedPath {
			t.Errorf("Expected %s, got: %s", expectedPath, result)
		}

		// Verify the parent directory doesn't exist yet
		if _, err := os.Stat(nonExistentDir); !os.IsNotExist(err) {
			t.Errorf("Expected parent directory %s to not exist yet, but it does", nonExistentDir)
		}
	})
}
