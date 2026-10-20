package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/arpitbhayani/pagebox/internal/ui"
)

//go:embed VERSION
var rawVersion string

var version = func() string {
	v := strings.TrimSpace(rawVersion)
	if v != "" {
		return v
	}
	return "0.1.2"
}()

const (
	defaultRepo       = "arpitbbhayani/pagebox"
	updateCheckPeriod = 24 * time.Hour
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the pagebox version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("pagebox version %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
	},
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and install the latest version of pagebox",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runSelfUpdate(version); err != nil {
			ui.PrintErr("update failed", err.Error(), os.Stderr)
			os.Exit(1)
		}
	},
}

// githubRelease describes the GitHub Releases API payload for the latest release.
type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// updateState stores the timestamp of the last daily update check and the latest version string seen.
type updateState struct {
	LastChecked time.Time `json:"last_checked"`
	LatestVer   string    `json:"latest_ver"`
}

func getRepoName() string {
	if r := os.Getenv("PAGEBOX_REPO"); r != "" {
		return r
	}
	return defaultRepo
}

func stateFilePath() string {
	// Respect XDG_STATE_HOME or fallback to ~/.pagebox/update_check.json
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "pagebox", "update_check.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "pagebox_update_check.json")
	}
	return filepath.Join(home, ".pagebox", "update_check.json")
}

func readUpdateState() (*updateState, error) {
	p := stateFilePath()
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s updateState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeUpdateState(s *updateState) {
	p := stateFilePath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, data, 0644)
}

// compareSemver returns:
//
//	 1 if v1 > v2
//	-1 if v1 < v2
//	 0 if v1 == v2
func compareSemver(v1, v2 string) int {
	v1 = strings.TrimPrefix(strings.TrimSpace(v1), "v")
	v2 = strings.TrimPrefix(strings.TrimSpace(v2), "v")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(parts1) {
			numStr := strings.Split(parts1[i], "-")[0]
			n1, _ = strconv.Atoi(numStr)
		}
		if i < len(parts2) {
			numStr := strings.Split(parts2[i], "-")[0]
			n2, _ = strconv.Atoi(numStr)
		}
		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}
	return 0
}

// fetchLatestRelease queries the GitHub API or release endpoint for the latest version.
func fetchLatestRelease(repo string) (*githubRelease, error) {
	apiURL := os.Getenv("PAGEBOX_UPDATE_URL")
	if apiURL == "" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pagebox-updater")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no published releases found for %s yet", repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, apiURL)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func downloadAsset(client *http.Client, url string, dst io.Writer) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

func checksumFor(data []byte, assetName string) (string, error) {
	var checksum string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(strings.TrimPrefix(fields[1], "*"), "./")
		if name != assetName {
			continue
		}
		if checksum != "" {
			return "", fmt.Errorf("multiple checksums found for %s", assetName)
		}
		raw, err := hex.DecodeString(fields[0])
		if err != nil || len(raw) != sha256.Size {
			return "", fmt.Errorf("invalid checksum for %s", assetName)
		}
		checksum = strings.ToLower(fields[0])
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if checksum == "" {
		return "", fmt.Errorf("checksum not found for %s", assetName)
	}
	return checksum, nil
}

func downloadVerifiedAsset(client *http.Client, assetURL, checksumURL, assetName string, dst io.Writer) error {
	if checksumURL != "" {
		var checksums bytes.Buffer
		if err := downloadAsset(client, checksumURL, &checksums); err != nil {
			return fmt.Errorf("download checksums: %w", err)
		}
		expected, err := checksumFor(checksums.Bytes(), assetName)
		if err != nil {
			return err
		}

		hash := sha256.New()
		if err := downloadAsset(client, assetURL, io.MultiWriter(dst, hash)); err != nil {
			return fmt.Errorf("download binary: %w", err)
		}
		actual := hex.EncodeToString(hash.Sum(nil))
		if actual != expected {
			return fmt.Errorf("checksum mismatch for %s", assetName)
		}
		return nil
	}

	// If no checksums file was provided with release, download binary directly
	return downloadAsset(client, assetURL, dst)
}

// autoUpdate runs in a background goroutine after the server has already started,
// ensuring pagebox never blocks on network calls at startup. It reuses the cached
// daily-check state so it only checks the network once per 24 hours. Any failure is
// non-fatal.
func autoUpdate(currentVersion string) {
	state, _ := readUpdateState()
	now := time.Now()

	var rel *githubRelease
	latestVer := ""
	if state != nil {
		latestVer = state.LatestVer
	}

	dueForCheck := state == nil || now.Sub(state.LastChecked) >= updateCheckPeriod
	if dueForCheck {
		var err error
		rel, err = fetchLatestRelease(getRepoName())
		if err != nil {
			return
		}
		latestVer = strings.TrimPrefix(rel.TagName, "v")
		writeUpdateState(&updateState{LastChecked: now, LatestVer: latestVer})
	}

	if latestVer == "" || compareSemver(latestVer, currentVersion) <= 0 {
		return
	}

	if rel == nil {
		var err error
		rel, err = fetchLatestRelease(getRepoName())
		if err != nil {
			return
		}
		latestVer = strings.TrimPrefix(rel.TagName, "v")
		if compareSemver(latestVer, currentVersion) <= 0 {
			return
		}
	}

	execPath, err := os.Executable()
	if err != nil {
		return
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return
	}

	ui.PrintStep(fmt.Sprintf("updating pagebox v%s -> v%s...", currentVersion, latestVer), "pass --no-update to skip", os.Stdout)

	if err := installUpdate(rel, latestVer, execPath); err != nil {
		ui.PrintWarn(fmt.Sprintf("auto-update failed: %v", err), "continuing with current version", os.Stderr)
		return
	}

	writeUpdateState(&updateState{LastChecked: time.Now(), LatestVer: latestVer})
	ui.PrintOk(fmt.Sprintf("pagebox has been updated to v%s", latestVer), "restart pagebox to see latest changes", os.Stdout)
}

// runSelfUpdate implements pagebox update.
func runSelfUpdate(currentVer string) error {
	repo := getRepoName()
	ui.PrintStep(fmt.Sprintf("checking for updates from %s...", repo), "", os.Stdout)

	rel, err := fetchLatestRelease(repo)
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latestVer := strings.TrimPrefix(rel.TagName, "v")
	if compareSemver(latestVer, currentVer) <= 0 {
		ui.PrintOk(fmt.Sprintf("pagebox is already up to date (v%s)", currentVer), "", os.Stdout)
		return nil
	}

	ui.PrintInfo(fmt.Sprintf("found newer version v%s (current: v%s)", latestVer, currentVer), "", os.Stdout)

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine running binary location: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("could not resolve executable symlink: %w", err)
	}

	if err := installUpdate(rel, latestVer, execPath); err != nil {
		return err
	}

	writeUpdateState(&updateState{
		LastChecked: time.Now(),
		LatestVer:   latestVer,
	})

	ui.PrintOk(fmt.Sprintf("pagebox successfully updated to v%s at %s", latestVer, execPath), "", os.Stdout)
	return nil
}

// installUpdate downloads, verifies, and atomically installs the release binary.
func installUpdate(rel *githubRelease, latestVer, execPath string) error {
	repo := getRepoName()
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	expectedAsset := fmt.Sprintf("pagebox-%s-%s-%s%s", latestVer, runtime.GOOS, runtime.GOARCH, ext)

	var downloadURL, checksumURL string
	for _, asset := range rel.Assets {
		switch asset.Name {
		case expectedAsset:
			downloadURL = asset.BrowserDownloadURL
		case "checksums.txt":
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, rel.TagName, expectedAsset)
	}

	ui.PrintStep(fmt.Sprintf("downloading %s...", expectedAsset), "", os.Stdout)

	dir := filepath.Dir(execPath)
	tmpFile, err := os.CreateTemp(dir, "pagebox-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied writing to %s. Try running with 'sudo pagebox update'", dir)
		}
		tmpFile, err = os.CreateTemp("", "pagebox-update-*")
		if err != nil {
			return fmt.Errorf("could not create temporary file: %w", err)
		}
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	client := &http.Client{Timeout: 60 * time.Second}
	if err := downloadVerifiedAsset(client, downloadURL, checksumURL, expectedAsset, tmpFile); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download update: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("saving downloaded binary: %w", err)
	}

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("setting executable permissions: %w", err)
	}

	// Verify the downloaded binary works
	cmd := exec.Command(tmpPath, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verification of new binary failed: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}

	// Atomically replace existing binary
	if runtime.GOOS == "windows" {
		oldPath := execPath + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(execPath, oldPath); err != nil {
			return fmt.Errorf("failed to move current binary on Windows: %w", err)
		}
		if err := copyOrMove(tmpPath, execPath); err != nil {
			_ = os.Rename(oldPath, execPath)
			return fmt.Errorf("failed to place new binary: %w", err)
		}
	} else {
		if err := os.Rename(tmpPath, execPath); err != nil {
			if err := copyOrMove(tmpPath, execPath); err != nil {
				if os.IsPermission(err) {
					return fmt.Errorf("permission denied replacing %s. Try running with 'sudo pagebox update'", execPath)
				}
				return fmt.Errorf("failed to replace binary %s: %w", execPath, err)
			}
		}
	}

	return nil
}

func copyOrMove(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, "pagebox-replace-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	return os.Rename(tmpName, dst)
}
