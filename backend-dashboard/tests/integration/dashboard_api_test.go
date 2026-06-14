package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestDashboardAPI_ReadEndpointsSmoke(t *testing.T) {
	db := newDashboardFixture()
	srv := newDashboardTestServer(t, db)
	defer srv.Close()

	client := srv.Client()
	endpoints := []struct {
		path       string
		wantStatus int
	}{
		{"/api/v1/overview", http.StatusOK},
		{"/api/v1/health", http.StatusOK},
		{"/api/v1/pipeline", http.StatusOK},
		{"/api/v1/positions", http.StatusOK},
		{"/api/v1/pnl", http.StatusOK},
		{"/api/v1/dq", http.StatusOK},
		{"/api/v1/activity", http.StatusOK},
		{"/api/v1/gate/evidence", http.StatusOK},
		{"/api/v1/configs", http.StatusOK},
	}

	for _, ep := range endpoints {
		ep := ep
		t.Run(ep.path, func(t *testing.T) {
			req := dashboardAuthRequest(t, http.MethodGet, srv.URL+ep.path, nil)
			res, err := client.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", ep.path, err)
			}
			defer res.Body.Close()
			if res.StatusCode != ep.wantStatus {
				body, _ := io.ReadAll(res.Body)
				t.Fatalf("GET %s status = %d, want %d, body = %s", ep.path, res.StatusCode, ep.wantStatus, body)
			}
			if ep.path == "/api/v1/overview" {
				var payload map[string]any
				if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
					t.Fatalf("decode overview: %v", err)
				}
				if payload["mode"] != "BALANCED" {
					t.Fatalf("overview mode = %v", payload["mode"])
				}
			}
		})
	}
}

func TestDashboardAPI_UnauthorizedWithoutKey(t *testing.T) {
	db := newDashboardFixture()
	srv := newDashboardTestServer(t, db)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/overview", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
}

func TestDashboardAPI_HealthExemptWithoutKey(t *testing.T) {
	db := newDashboardFixture()
	srv := newDashboardTestServer(t, db)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", res.StatusCode)
	}
}
