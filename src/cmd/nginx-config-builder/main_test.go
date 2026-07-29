package main

import (
	"dokku-nginx-custom/src/pkg/file_config"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAllocateNextReleaseDirectory(t *testing.T) {
	today := time.Now().Format("20060102")

	t.Run("FirstReleaseWhenEmpty", func(t *testing.T) {
		dir := t.TempDir()
		got, err := allocateNextReleaseDirectory(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(dir, fmt.Sprintf("release-%s.1", today))
		if got != want {
			t.Fatalf("expected %s, got %s", want, got)
		}
	})

	t.Run("IncrementsSequenceForToday", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{
			fmt.Sprintf("release-%s.1", today),
			fmt.Sprintf("release-%s.2", today),
		} {
			if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
				t.Fatal(err)
			}
		}
		got, err := allocateNextReleaseDirectory(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(dir, fmt.Sprintf("release-%s.3", today))
		if got != want {
			t.Fatalf("expected %s, got %s", want, got)
		}
	})

	t.Run("IgnoresOtherDatesWhenAllocatingToday", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"release-20011225.9", "release-20011224.3"} {
			if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
				t.Fatal(err)
			}
		}
		got, err := allocateNextReleaseDirectory(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(dir, fmt.Sprintf("release-%s.1", today))
		if got != want {
			t.Fatalf("expected %s, got %s", want, got)
		}
	})

	t.Run("IgnoresInvalidNamesAndFiles", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"release-1.0.0", "not-a-release", fmt.Sprintf("release-%s.1", today)} {
			if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("release-%s.99", today)), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := allocateNextReleaseDirectory(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(dir, fmt.Sprintf("release-%s.2", today))
		if got != want {
			t.Fatalf("expected %s, got %s", want, got)
		}
	})

	t.Run("UsesMaxSequenceNotCount", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{
			fmt.Sprintf("release-%s.1", today),
			fmt.Sprintf("release-%s.5", today),
		} {
			if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
				t.Fatal(err)
			}
		}
		got, err := allocateNextReleaseDirectory(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(dir, fmt.Sprintf("release-%s.6", today))
		if got != want {
			t.Fatalf("expected %s, got %s", want, got)
		}
	})
}

func TestPruneOldReleases(t *testing.T) {
	mkdirReleases := func(t *testing.T, dir string, names ...string) {
		t.Helper()
		for _, name := range names {
			if err := os.MkdirAll(filepath.Join(dir, name), 0755); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(dir, name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}

	t.Run("KeepsCurrentPlusRetainCountNewest", func(t *testing.T) {
		dir := t.TempDir()
		mkdirReleases(t, dir,
			"release-20011220.1",
			"release-20011221.1",
			"release-20011222.1",
			"release-20011223.1",
			"release-20011224.1",
		)
		current := filepath.Join(dir, "release-20011224.1")
		if err := pruneOldReleases(dir, current, 2); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists(dir, "release-20011224.1") || !exists(dir, "release-20011223.1") || !exists(dir, "release-20011222.1") {
			t.Fatal("expected current and two newest non-current to remain")
		}
		if exists(dir, "release-20011221.1") || exists(dir, "release-20011220.1") {
			t.Fatal("expected older releases to be pruned")
		}
	})

	t.Run("RetainZeroDeletesAllNonCurrent", func(t *testing.T) {
		dir := t.TempDir()
		mkdirReleases(t, dir, "release-20011220.1", "release-20011224.1")
		current := filepath.Join(dir, "release-20011224.1")
		if err := pruneOldReleases(dir, current, 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists(dir, "release-20011224.1") {
			t.Fatal("current must remain")
		}
		if exists(dir, "release-20011220.1") {
			t.Fatal("non-current should be deleted when retain=0")
		}
	})

	t.Run("NoopWhenWithinLimit", func(t *testing.T) {
		dir := t.TempDir()
		mkdirReleases(t, dir, "release-20011220.1", "release-20011224.1")
		current := filepath.Join(dir, "release-20011224.1")
		if err := pruneOldReleases(dir, current, 10); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists(dir, "release-20011220.1") || !exists(dir, "release-20011224.1") {
			t.Fatal("expected all releases retained")
		}
	})

	t.Run("RejectsNegativeRetainCount", func(t *testing.T) {
		dir := t.TempDir()
		if err := pruneOldReleases(dir, filepath.Join(dir, "release-x"), -1); err == nil {
			t.Fatal("expected error for negative retain count")
		}
	})
}

func TestResolveOldConfigRetainCount(t *testing.T) {
	t.Run("DefaultsToTen", func(t *testing.T) {
		got, err := resolveOldConfigRetainCount(&file_config.Config{}, "")
		if err != nil || got != 10 {
			t.Fatalf("expected 10, got %d err=%v", got, err)
		}
	})

	t.Run("UsesPropertyWhenYamlUnset", func(t *testing.T) {
		got, err := resolveOldConfigRetainCount(&file_config.Config{}, "3")
		if err != nil || got != 3 {
			t.Fatalf("expected 3, got %d err=%v", got, err)
		}
	})

	t.Run("YamlOverridesProperty", func(t *testing.T) {
		n := 7
		got, err := resolveOldConfigRetainCount(&file_config.Config{OldConfigRetainCount: &n}, "3")
		if err != nil || got != 7 {
			t.Fatalf("expected 7, got %d err=%v", got, err)
		}
	})

	t.Run("RejectsNegativeYaml", func(t *testing.T) {
		n := -1
		if _, err := resolveOldConfigRetainCount(&file_config.Config{OldConfigRetainCount: &n}, "3"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("RejectsInvalidProperty", func(t *testing.T) {
		if _, err := resolveOldConfigRetainCount(&file_config.Config{}, "nope"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveAutoRollback(t *testing.T) {
	t.Run("DefaultsToTrue", func(t *testing.T) {
		got, err := resolveAutoRollback(&file_config.Config{}, "")
		if err != nil || !got {
			t.Fatalf("expected true, got %v err=%v", got, err)
		}
	})

	t.Run("PropertyFalse", func(t *testing.T) {
		got, err := resolveAutoRollback(&file_config.Config{}, "false")
		if err != nil || got {
			t.Fatalf("expected false, got %v err=%v", got, err)
		}
	})

	t.Run("YamlOverridesProperty", func(t *testing.T) {
		v := false
		got, err := resolveAutoRollback(&file_config.Config{AutoRollback: &v}, "true")
		if err != nil || got {
			t.Fatalf("expected false from yaml, got %v err=%v", got, err)
		}
	})

	t.Run("RejectsInvalidProperty", func(t *testing.T) {
		if _, err := resolveAutoRollback(&file_config.Config{}, "maybe"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveFailedConfigRetainCount(t *testing.T) {
	t.Run("DefaultsToTen", func(t *testing.T) {
		got, err := resolveFailedConfigRetainCount(&file_config.Config{}, "")
		if err != nil || got != 10 {
			t.Fatalf("expected 10, got %d err=%v", got, err)
		}
	})

	t.Run("YamlOverridesProperty", func(t *testing.T) {
		n := 2
		got, err := resolveFailedConfigRetainCount(&file_config.Config{FailedConfigRetainCount: &n}, "9")
		if err != nil || got != 2 {
			t.Fatalf("expected 2, got %d err=%v", got, err)
		}
	})
}

func TestQuarantineAndRollback(t *testing.T) {
	exists := func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	}
	readlink := func(t *testing.T, dir string) string {
		t.Helper()
		target, err := os.Readlink(filepath.Join(dir, "current"))
		if err != nil {
			t.Fatalf("readlink: %v", err)
		}
		return target
	}

	t.Run("QuarantineMovesReleaseUnderFailed", func(t *testing.T) {
		dir := t.TempDir()
		rel := filepath.Join(dir, "release-20011225.1")
		if err := os.MkdirAll(filepath.Join(rel, "vhosts"), 0755); err != nil {
			t.Fatal(err)
		}
		dest, err := quarantineFailedRelease(dir, rel)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(dir, "failed", "release-20011225.1")
		if dest != want {
			t.Fatalf("expected %s, got %s", want, dest)
		}
		if exists(rel) {
			t.Fatal("source release should be gone")
		}
		if !exists(want) {
			t.Fatal("quarantined release missing")
		}
	})

	t.Run("PruneFailedKeepsNewest", func(t *testing.T) {
		dir := t.TempDir()
		failed := filepath.Join(dir, "failed")
		for _, name := range []string{"release-20011220.1", "release-20011221.1", "release-20011222.1"} {
			if err := os.MkdirAll(filepath.Join(failed, name), 0755); err != nil {
				t.Fatal(err)
			}
		}
		if err := pruneFailedReleases(dir, 1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !exists(filepath.Join(failed, "release-20011222.1")) {
			t.Fatal("newest failed should remain")
		}
		if exists(filepath.Join(failed, "release-20011220.1")) || exists(filepath.Join(failed, "release-20011221.1")) {
			t.Fatal("older failed should be pruned")
		}
	})

	t.Run("HandleFailureRestoresPreviousAndQuarantines", func(t *testing.T) {
		dir := t.TempDir()
		prev := filepath.Join(dir, "release-20011224.1")
		neu := filepath.Join(dir, "release-20011225.1")
		if err := os.MkdirAll(prev, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(neu, 0755); err != nil {
			t.Fatal(err)
		}
		if err := updateCurrentSymlink(dir, neu); err != nil {
			t.Fatal(err)
		}
		testErr := fmt.Errorf("nginx config test failed: boom")
		err := handleNginxTestFailure(dir, neu, prev, true, 10, testErr)
		if err == nil {
			t.Fatal("expected test error returned")
		}
		if readlink(t, dir) != "release-20011224.1" {
			t.Fatalf("expected current restored to previous, got %s", readlink(t, dir))
		}
		if exists(neu) {
			t.Fatal("failed release should be quarantined")
		}
		if !exists(filepath.Join(dir, "failed", "release-20011225.1")) {
			t.Fatal("expected quarantine under failed/")
		}
	})

	t.Run("HandleFailureFirstDeployRemovesCurrent", func(t *testing.T) {
		dir := t.TempDir()
		neu := filepath.Join(dir, "release-20011225.1")
		if err := os.MkdirAll(neu, 0755); err != nil {
			t.Fatal(err)
		}
		if err := updateCurrentSymlink(dir, neu); err != nil {
			t.Fatal(err)
		}
		_ = handleNginxTestFailure(dir, neu, "", true, 10, fmt.Errorf("boom"))
		if _, err := os.Lstat(filepath.Join(dir, "current")); !os.IsNotExist(err) {
			t.Fatalf("expected current removed, lstat err=%v", err)
		}
		if !exists(filepath.Join(dir, "failed", "release-20011225.1")) {
			t.Fatal("expected quarantine")
		}
	})

	t.Run("HandleFailureOptOutLeavesCurrentNoQuarantine", func(t *testing.T) {
		dir := t.TempDir()
		neu := filepath.Join(dir, "release-20011225.1")
		if err := os.MkdirAll(neu, 0755); err != nil {
			t.Fatal(err)
		}
		if err := updateCurrentSymlink(dir, neu); err != nil {
			t.Fatal(err)
		}
		_ = handleNginxTestFailure(dir, neu, "", false, 10, fmt.Errorf("boom"))
		if readlink(t, dir) != "release-20011225.1" {
			t.Fatal("opt-out should leave current on failed release")
		}
		if exists(filepath.Join(dir, "failed")) {
			t.Fatal("opt-out should not create failed/")
		}
		if !exists(neu) {
			t.Fatal("release should remain in place")
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

		err := copyConfigToRelease(configContent, releaseDir, filename, 0644, chown{})
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

		err := copyConfigToRelease(configContent, releaseDir, filename, 0644, chown{})
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

func TestExpandLocationConfigsWithAliases(t *testing.T) {
	t.Run("DuplicatesConfigForEachAdditionalServerName", func(t *testing.T) {
		locationConfigs := vhostToLocationConfigStringMap{
			"api.example.com": "proxy_pass http://upstream;",
		}
		vhosts := []file_config.VhostConfig{
			{
				ServerName:            "api.example.com",
				AdditionalServerNames: []string{"api.example.org", "api-alias.example.com"},
			},
		}

		got, err := expandLocationConfigsWithAliases(locationConfigs, vhosts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantBodies := map[string]string{
			"api.example.com":       "proxy_pass http://upstream;",
			"api.example.org":       "proxy_pass http://upstream;",
			"api-alias.example.com": "proxy_pass http://upstream;",
		}
		if len(got) != len(wantBodies) {
			t.Fatalf("expected %d host entries, got %d: %#v", len(wantBodies), len(got), got)
		}
		for host, body := range wantBodies {
			if got[host] != body {
				t.Fatalf("host %q: expected %q, got %q", host, body, got[host])
			}
		}
	})

	t.Run("NoopWhenNoAdditionalNames", func(t *testing.T) {
		locationConfigs := vhostToLocationConfigStringMap{
			"api.example.com": "ok",
		}
		got, err := expandLocationConfigsWithAliases(locationConfigs, []file_config.VhostConfig{
			{ServerName: "api.example.com"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["api.example.com"] != "ok" {
			t.Fatalf("unexpected result: %#v", got)
		}
	})

	t.Run("ErrorsWhenAliasDuplicatesOwnServerName", func(t *testing.T) {
		_, err := expandLocationConfigsWithAliases(
			vhostToLocationConfigStringMap{"api.example.com": "ok"},
			[]file_config.VhostConfig{{
				ServerName:            "api.example.com",
				AdditionalServerNames: []string{"api.example.com"},
			}},
		)
		if err == nil {
			t.Fatal("expected error when alias duplicates own server_name")
		}
	})

	t.Run("ErrorsWhenAliasCollidesWithAnotherServerName", func(t *testing.T) {
		_, err := expandLocationConfigsWithAliases(
			vhostToLocationConfigStringMap{
				"api.example.com": "a",
				"www.example.com": "b",
			},
			[]file_config.VhostConfig{
				{
					ServerName:            "api.example.com",
					AdditionalServerNames: []string{"www.example.com"},
				},
				{ServerName: "www.example.com"},
			},
		)
		if err == nil {
			t.Fatal("expected error when alias collides with another server_name")
		}
	})

	t.Run("ErrorsWhenSameAliasClaimedTwice", func(t *testing.T) {
		_, err := expandLocationConfigsWithAliases(
			vhostToLocationConfigStringMap{
				"a.example.com": "a",
				"b.example.com": "b",
			},
			[]file_config.VhostConfig{
				{
					ServerName:            "a.example.com",
					AdditionalServerNames: []string{"shared.example.com"},
				},
				{
					ServerName:            "b.example.com",
					AdditionalServerNames: []string{"shared.example.com"},
				},
			},
		)
		if err == nil {
			t.Fatal("expected error when same alias is claimed by two vhosts")
		}
	})
}
