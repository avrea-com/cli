package client

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avrea-com/cli/config"
)

// OrganizationMembers keeps its one-page contract for existing Go callers.
func TestOrganizationMembersReadsOnePage(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.RequestURI())
		_, _ = fmt.Fprint(w, `{"data":[{"user_id":"usr-1"}],"pagination":{"next_cursor":"next"}}`)
	}))
	defer server.Close()
	api := New(config.Config{Host: server.URL, Token: "secret"}, server.Client(), "avr-test")
	page, err := api.OrganizationMembers(t.Context(), "acme/eu")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0] != "/orgs/acme%2Feu/members" || len(page.Items) != 1 || page.Items[0]["user_id"] != "usr-1" || page.NextCursor != "next" {
		t.Fatal(requests, page)
	}
}
