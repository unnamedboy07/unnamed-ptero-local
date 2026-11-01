package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const mojangManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type Installer struct{ client *http.Client }

func NewInstaller() *Installer { return &Installer{client: &http.Client{Timeout: 15 * time.Minute}} }

type mojangManifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"versions"`
}
type mojangVersion struct {
	JavaVersion struct {
		Major int `json:"majorVersion"`
	} `json:"javaVersion"`
	Downloads struct {
		Server struct {
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
		} `json:"server"`
	} `json:"downloads"`
}

func (i *Installer) Versions(ctx context.Context, software string) ([]string, string, error) {
	software = strings.ToLower(strings.TrimSpace(software))
	switch software {
	case "", "vanilla":
		var m mojangManifest
		if err := i.getJSON(ctx, mojangManifestURL, &m); err != nil {
			return nil, "", err
		}
		out := make([]string, 0, 40)
		for _, v := range m.Versions {
			if v.Type == "release" {
				out = append(out, v.ID)
				if len(out) == 40 {
					break
				}
			}
		}
		if len(out) == 0 {
			return nil, "", fmt.Errorf("Mojang did not return any release versions")
		}
		return out, m.Latest.Release, nil

	case "paper":
		// PaperMC retired api.papermc.io/v2. The supported Fill API is hosted
		// separately at fill.papermc.io and groups versions by major/minor.
		var project struct {
			Versions map[string][]string `json:"versions"`
		}
		if err := i.getJSON(ctx, "https://fill.papermc.io/v3/projects/paper", &project); err != nil {
			return nil, "", fmt.Errorf("fetch PaperMC version catalogue: %w", err)
		}
		out := make([]string, 0, 40)
		seen := make(map[string]bool)
		for _, group := range project.Versions {
			for _, version := range group {
				if isStableMinecraftVersion(version) && !seen[version] {
					seen[version] = true
					out = append(out, version)
				}
			}
		}
		sort.Slice(out, func(a, b int) bool { return minecraftVersionGreater(out[a], out[b]) })
		if len(out) == 0 {
			return nil, "", fmt.Errorf("PaperMC did not return any stable Minecraft versions")
		}
		return out, out[0], nil

	case "purpur":
		var project struct {
			Versions []string `json:"versions"`
		}
		if err := i.getJSON(ctx, "https://api.purpurmc.org/v2/purpur", &project); err != nil {
			return nil, "", fmt.Errorf("fetch Purpur version catalogue: %w", err)
		}
		out := make([]string, 0, len(project.Versions))
		for _, version := range project.Versions {
			if isStableMinecraftVersion(version) {
				out = append(out, version)
			}
		}
		sort.Slice(out, func(a, b int) bool { return minecraftVersionGreater(out[a], out[b]) })
		if len(out) == 0 {
			return nil, "", fmt.Errorf("Purpur did not return any stable Minecraft versions")
		}
		latest := out[0]
		return out, latest, nil

	case "fabric":
		var catalogue []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := i.getJSON(ctx, "https://meta.fabricmc.net/v2/versions/game", &catalogue); err != nil {
			return nil, "", fmt.Errorf("fetch Fabric version catalogue: %w", err)
		}
		out := make([]string, 0, 40)
		for _, entry := range catalogue {
			if entry.Stable && isStableMinecraftVersion(entry.Version) {
				out = append(out, entry.Version)
			}
		}
		if len(out) == 0 {
			return nil, "", fmt.Errorf("Fabric did not return any stable Minecraft versions")
		}
		// Fabric's API returns newest first. Keep that provider ordering.
		return out, out[0], nil

	default:
		return nil, "", fmt.Errorf("unsupported server software %q", software)
	}
}

func isStableMinecraftVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func minecraftVersionGreater(a, b string) bool {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for idx := 0; idx < len(ap) || idx < len(bp); idx++ {
		av, bv := 0, 0
		if idx < len(ap) {
			_, _ = fmt.Sscan(ap[idx], &av)
		}
		if idx < len(bp) {
			_, _ = fmt.Sscan(bp[idx], &bv)
		}
		if av != bv {
			return av > bv
		}
	}
	return a > b
}

func (i *Installer) RequiredJava(ctx context.Context, version string) (int, error) {
	var manifest mojangManifest
	if err := i.getJSON(ctx, mojangManifestURL, &manifest); err != nil {
		return 0, err
	}
	var metadataURL string
	for _, item := range manifest.Versions {
		if item.ID == version {
			metadataURL = item.URL
			break
		}
	}
	if metadataURL == "" {
		return 0, fmt.Errorf("Minecraft version %q was not found in Mojang's manifest", version)
	}
	var details mojangVersion
	if err := i.getJSON(ctx, metadataURL, &details); err != nil {
		return 0, err
	}
	if details.JavaVersion.Major <= 0 {
		return javaFallback(version), nil
	}
	return details.JavaVersion.Major, nil
}

func javaFallback(version string) int {
	var major, minor, patch int
	if strings.HasPrefix(version, "1.") {
		parts := strings.Split(version, ".")
		if len(parts) > 1 {
			_, _ = fmt.Sscan(parts[1], &minor)
		}
		if len(parts) > 2 {
			_, _ = fmt.Sscan(parts[2], &patch)
		}
		if minor <= 16 {
			return 8
		}
		if minor == 17 {
			return 16
		}
		if minor == 18 || minor == 19 || minor == 20 {
			if minor == 20 && patch >= 5 {
				return 21
			}
			return 17
		}
		if minor >= 21 {
			return 21
		}
	}
	if _, err := fmt.Sscan(strings.Split(version, ".")[0], &major); err == nil && major >= 26 {
		return 25
	}
	return 21
}

func (i *Installer) DownloadJar(ctx context.Context, software, version, destination string) (string, error) {
	software = strings.ToLower(strings.TrimSpace(software))
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("Minecraft version is required")
	}
	var downloadURL string
	switch software {
	case "vanilla":
		var m mojangManifest
		if err := i.getJSON(ctx, mojangManifestURL, &m); err != nil {
			return "", fmt.Errorf("fetch Mojang version list: %w", err)
		}
		if version == "latest" {
			version = m.Latest.Release
		}
		var versionURL string
		for _, v := range m.Versions {
			if v.ID == version {
				versionURL = v.URL
				break
			}
		}
		if versionURL == "" {
			return "", fmt.Errorf("Minecraft version %q was not found in Mojang's manifest", version)
		}
		var details mojangVersion
		if err := i.getJSON(ctx, versionURL, &details); err != nil {
			return "", fmt.Errorf("fetch Minecraft version metadata: %w", err)
		}
		downloadURL = details.Downloads.Server.URL
		if downloadURL == "" {
			return "", fmt.Errorf("Mojang does not publish a server jar for version %s", version)
		}
	case "paper":
		// PaperMC's v2 API is permanently retired. Use the v3 Fill service.
		// Its build objects contain the canonical download URL and SHA-256.
		buildsURL := "https://fill.papermc.io/v3/projects/paper/versions/" + url.PathEscape(version) + "/builds"
		var builds []struct {
			ID        int    `json:"id"`
			Channel   string `json:"channel"`
			Downloads map[string]struct {
				Name      string `json:"name"`
				URL       string `json:"url"`
				Checksums struct {
					SHA256 string `json:"sha256"`
				} `json:"checksums"`
			} `json:"downloads"`
		}
		if err := i.getJSON(ctx, buildsURL, &builds); err != nil {
			return "", fmt.Errorf("fetch Paper builds for Minecraft %s: %w", version, err)
		}
		sort.Slice(builds, func(a, b int) bool { return builds[a].ID > builds[b].ID })
		for _, build := range builds {
			if !strings.EqualFold(build.Channel, "STABLE") {
				continue
			}
			download := build.Downloads["server:default"]
			if download.URL == "" {
				continue
			}
			downloadURL = download.URL
			if err := i.download(ctx, downloadURL, destination, download.Checksums.SHA256); err != nil {
				return "", fmt.Errorf("download Paper %s build %d: %w", version, build.ID, err)
			}
			return "server.jar", nil
		}
		return "", fmt.Errorf("Paper has no stable server build for Minecraft %s yet; choose another supported version", version)
	case "purpur":
		u := "https://api.purpurmc.org/v2/purpur/" + url.PathEscape(version)
		var result struct {
			Builds struct {
				Latest string `json:"latest"`
			} `json:"builds"`
		}
		if err := i.getJSON(ctx, u, &result); err != nil {
			return "", fmt.Errorf("Purpur does not list version %s: %w", version, err)
		}
		if result.Builds.Latest == "" {
			return "", fmt.Errorf("no Purpur build found for Minecraft %s", version)
		}
		downloadURL = fmt.Sprintf("https://api.purpurmc.org/v2/purpur/%s/%s/download", url.PathEscape(version), url.PathEscape(result.Builds.Latest))
	case "fabric":
		u := "https://meta.fabricmc.net/v2/versions/loader/" + url.PathEscape(version)
		var loaders []struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}
		if err := i.getJSON(ctx, u, &loaders); err != nil {
			return "", fmt.Errorf("fetch Fabric loader versions: %w", err)
		}
		if len(loaders) == 0 {
			return "", fmt.Errorf("Fabric does not list a loader for Minecraft %s", version)
		}
		var installers []struct {
			Version string `json:"version"`
		}
		if err := i.getJSON(ctx, "https://meta.fabricmc.net/v2/versions/installer", &installers); err != nil {
			return "", fmt.Errorf("fetch Fabric installer versions: %w", err)
		}
		if len(installers) == 0 {
			return "", fmt.Errorf("no Fabric installer is available")
		}
		downloadURL = fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar", url.PathEscape(version), url.PathEscape(loaders[0].Loader.Version), url.PathEscape(installers[0].Version))
	default:
		return "", fmt.Errorf("unsupported server software %q", software)
	}
	if err := i.download(ctx, downloadURL, destination); err != nil {
		return "", err
	}
	return "server.jar", nil
}

func (i *Installer) getJSON(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "UnnamedPtero/0.3.2 (https://github.com/unnamedthings07)")
	req.Header.Set("Accept", "application/json")
	resp, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s returned HTTP %d: %s", target, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(out)
}

func (i *Installer) download(ctx context.Context, target, destination string, expectedSHA256 ...string) error {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS server download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "UnnamedPtero/0.3.2 (https://github.com/unnamedthings07)")
	req.Header.Set("Accept", "*/*")
	resp, err := i.client.Do(req)
	if err != nil {
		return fmt.Errorf("download server jar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server download returned HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp := destination + ".download"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, 2<<30))
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return closeErr
	}
	if len(expectedSHA256) > 0 && expectedSHA256[0] != "" {
		actual := hex.EncodeToString(hash.Sum(nil))
		if !strings.EqualFold(actual, expectedSHA256[0]) {
			os.Remove(tmp)
			return fmt.Errorf("download checksum mismatch: expected %s, got %s", expectedSHA256[0], actual)
		}
	}
	st, err := os.Stat(tmp)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if st.Size() < 100_000 {
		os.Remove(tmp)
		return fmt.Errorf("downloaded server jar is unexpectedly small (%d bytes)", st.Size())
	}
	if err := os.Rename(tmp, destination); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
