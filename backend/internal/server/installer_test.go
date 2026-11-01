package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func TestIsStableMinecraftVersion(t *testing.T) {
	cases := map[string]bool{
		"26.3":            true,
		"26.1.2":          true,
		"1.21.11":         true,
		"1.8.8":           true,
		"26.3-rc-1":       false,
		"26.4-snapshot-6": false,
		"26w14a":          false,
		"latest":          false,
		"1":               false,
	}
	for version, want := range cases {
		if got := isStableMinecraftVersion(version); got != want {
			t.Errorf("isStableMinecraftVersion(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestMinecraftVersionGreaterUsesNumericParts(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"26.3", "26.2", true},
		{"26.10", "26.3", true},
		{"1.21.11", "1.21.9", true},
		{"1.20.6", "1.21", false},
		{"26.2", "26.2", false},
	}
	for _, tc := range cases {
		if got := minecraftVersionGreater(tc.a, tc.b); got != tc.want {
			t.Errorf("minecraftVersionGreater(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestPaperVersionCatalogueUsesFillV3(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://fill.papermc.io/v3/projects/paper" {
			return response(req, http.StatusNotFound, []byte("unexpected URL: "+req.URL.String())), nil
		}
		if !strings.Contains(req.Header.Get("User-Agent"), "UnnamedPtero/") {
			t.Fatalf("expected identifying User-Agent, got %q", req.Header.Get("User-Agent"))
		}
		body := []byte(`{"versions":{"26.3":["26.3","26.3-rc-3"],"26.2":["26.2","26.2-rc-2"],"1.21":["1.21.11"]}}`)
		return response(req, http.StatusOK, body), nil
	})}
	installer := &Installer{client: client}
	versions, latest, err := installer.Versions(context.Background(), "paper")
	if err != nil {
		t.Fatal(err)
	}
	if latest != "26.3" {
		t.Fatalf("latest = %q, want 26.3", latest)
	}
	got := strings.Join(versions, ",")
	want := "26.3,26.2,1.21.11"
	if got != want {
		t.Fatalf("versions = %q, want %q", got, want)
	}
}

func TestPaperDownloadUsesStableBuildAndVerifiesChecksum(t *testing.T) {
	jar := bytes.Repeat([]byte("localptero-paper-jar-"), 7000)
	sum := sha256.Sum256(jar)
	checksum := hex.EncodeToString(sum[:])
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/versions/26.2/builds"):
			body := fmt.Sprintf(`[
				{"id": 99, "channel": "BETA", "downloads": {"server:default": {"name":"paper-beta.jar", "url":"https://fill-data.papermc.io/beta.jar"}}},
				{"id": 98, "channel": "STABLE", "downloads": {"server:default": {"name":"paper-26.2-98.jar", "url":"https://fill-data.papermc.io/stable.jar", "checksums":{"sha256":"%s"}}}}
			]`, checksum)
			return response(req, http.StatusOK, []byte(body)), nil
		case req.URL.Host == "fill-data.papermc.io":
			if req.URL.Path != "/stable.jar" {
				t.Fatalf("expected stable artifact, got %s", req.URL.Path)
			}
			return response(req, http.StatusOK, jar), nil
		default:
			return response(req, http.StatusNotFound, []byte("unexpected URL: "+req.URL.String())), nil
		}
	})}
	installer := &Installer{client: client}
	destination := filepath.Join(t.TempDir(), "server.jar")
	name, err := installer.DownloadJar(context.Background(), "paper", "26.2", destination)
	if err != nil {
		t.Fatal(err)
	}
	if name != "server.jar" {
		t.Fatalf("jar name = %q, want server.jar", name)
	}
	actual, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, jar) {
		t.Fatal("downloaded jar contents differ from the stable artifact")
	}
}

func TestPaperDownloadRejectsChecksumMismatch(t *testing.T) {
	jar := bytes.Repeat([]byte("paper-jar"), 15000)
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/versions/26.2/builds") {
			body := `[{"id":1,"channel":"STABLE","downloads":{"server:default":{"name":"paper.jar","url":"https://fill-data.papermc.io/paper.jar","checksums":{"sha256":"not-the-real-checksum"}}}}]`
			return response(req, http.StatusOK, []byte(body)), nil
		}
		return response(req, http.StatusOK, jar), nil
	})}
	installer := &Installer{client: client}
	destination := filepath.Join(t.TempDir(), "server.jar")
	if _, err := installer.DownloadJar(context.Background(), "paper", "26.2", destination); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("unexpected server.jar after checksum failure, stat err=%v", err)
	}
}
