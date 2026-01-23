package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withLatestReleaseClient(t *testing.T, url string, client *http.Client) {
	t.Helper()
	origURL := latestReleaseURL
	origClient := httpClient
	latestReleaseURL = url
	httpClient = client
	t.Cleanup(func() {
		latestReleaseURL = origURL
		httpClient = origClient
	})
}

func withLatestReleaseServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	withLatestReleaseClient(t, server.URL, server.Client())
}

func TestCheckOutdated(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0"}`))
	})

	result, err := Check(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !result.Outdated {
		t.Fatalf("expected outdated, got %+v", result)
	}
	if result.Latest != "1.2.0" {
		t.Fatalf("expected latest 1.2.0, got %s", result.Latest)
	}
	if result.Current != "1.0.0" {
		t.Fatalf("expected current 1.0.0, got %s", result.Current)
	}
}

func TestCheckUpToDate(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	})

	result, err := Check(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if result.Outdated {
		t.Fatalf("expected up-to-date, got %+v", result)
	}
}

func TestCheckDevBuild(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	})

	result, err := Check(context.Background(), "dev")
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !result.CurrentIsDev {
		t.Fatalf("expected dev build, got %+v", result)
	}
	if result.Outdated {
		t.Fatalf("expected no outdated comparison for dev build, got %+v", result)
	}
	if result.Latest != "2.0.0" {
		t.Fatalf("expected latest 2.0.0, got %s", result.Latest)
	}
}

func TestCheckInvalidLatest(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"not-a-version"}`))
	})

	if _, err := Check(context.Background(), "1.0.0"); err == nil {
		t.Fatal("expected error for invalid latest tag")
	}
}

func TestCheckInvalidCurrentVersion(t *testing.T) {
	if _, err := Check(context.Background(), "not-a-version"); err == nil {
		t.Fatal("expected error for invalid current version")
	}
}

func TestCheckNewerThanLatest(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	})

	result, err := Check(context.Background(), "2.0.0")
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if result.Outdated {
		t.Fatalf("expected not outdated, got %+v", result)
	}
	if result.Current != "2.0.0" {
		t.Fatalf("expected current 2.0.0, got %s", result.Current)
	}
	if result.Latest != "1.0.0" {
		t.Fatalf("expected latest 1.0.0, got %s", result.Latest)
	}
}

func TestFetchLatestReleaseVersion_RequestError(t *testing.T) {
	withLatestReleaseClient(t, "http://[::1", http.DefaultClient)

	if _, err := fetchLatestReleaseVersion(context.Background()); err == nil {
		t.Fatal("expected error for invalid latest release URL")
	}
}

func TestFetchLatestReleaseVersion_DoError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}),
	}
	withLatestReleaseClient(t, "https://example.com", client)

	if _, err := fetchLatestReleaseVersion(context.Background()); err == nil {
		t.Fatal("expected error for failed latest release request")
	}
}

func TestFetchLatestReleaseVersion_StatusError(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := fetchLatestReleaseVersion(context.Background()); err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestFetchLatestReleaseVersion_DecodeError(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	})

	if _, err := fetchLatestReleaseVersion(context.Background()); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFetchLatestReleaseVersion_EmptyTag(t *testing.T) {
	withLatestReleaseServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":""}`))
	})

	if _, err := fetchLatestReleaseVersion(context.Background()); err == nil {
		t.Fatal("expected error for empty tag name")
	}
}

func TestCompareSemverInvalid(t *testing.T) {
	if _, err := compareSemver("1.2", "1.0.0"); err == nil {
		t.Fatal("expected error for invalid semantic version")
	}
}

func TestCompareSemverInvalidLatest(t *testing.T) {
	if _, err := compareSemver("1.0.0", "1.2"); err == nil {
		t.Fatal("expected error for invalid latest version")
	}
}

func TestParseSemverOverflow(t *testing.T) {
	if _, err := parseSemver("9999999999999999999999999.0.0"); err == nil {
		t.Fatal("expected error for overflowed version segment")
	}
}
