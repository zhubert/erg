# Label/Tag Filtering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add label/tag-based filtering for Asana (tags) and Linear (labels), reusing the existing `source.filter.label` workflow config field.

**Architecture:** Change `Provider.FetchIssues` to accept a `FilterConfig` struct (defined in the issues package). Asana adds `tags.name` to `opt_fields` and filters client-side. Linear adds a label filter to its GraphQL query. When label is empty, behavior is unchanged.

**Tech Stack:** Go, Asana REST API, Linear GraphQL API

---

### Task 1: Add FilterConfig to issues package and update Provider interface

**Files:**
- Modify: `internal/issues/provider.go:19-40`

**Step 1: Write the failing test**

No new test file needed — the existing `provider_test.go` mockProvider will fail to compile after the interface change, which is the expected failure signal. Skip to implementation.

**Step 2: Add FilterConfig and update the Provider interface**

In `internal/issues/provider.go`, add a `FilterConfig` struct and update `FetchIssues`:

```go
// FilterConfig holds provider-specific filter parameters for fetching issues.
type FilterConfig struct {
	Label   string // Tag/label name to filter by (empty = no filtering)
	Project string // Asana: project GID
	Team    string // Linear: team ID
}

// In Provider interface, change:
FetchIssues(ctx context.Context, repoPath string, filter FilterConfig) ([]Issue, error)
```

**Step 3: Update mockProvider in provider_test.go**

In `internal/issues/provider_test.go`, update the mock:

```go
func (m *mockProvider) FetchIssues(_ context.Context, _ string, _ FilterConfig) ([]Issue, error) {
```

**Step 4: Update GitHubProvider.FetchIssues**

In `internal/issues/github.go`, change the signature. The `filter` param is unused by GitHub (GitHub filtering happens in the daemon via `gh` CLI label arg):

```go
func (p *GitHubProvider) FetchIssues(ctx context.Context, repoPath string, filter FilterConfig) ([]Issue, error) {
```

**Step 5: Update AsanaProvider.FetchIssues signature (no filtering logic yet)**

In `internal/issues/asana.go`, change the signature. Extract `projectID` from `filter.Project`:

```go
func (p *AsanaProvider) FetchIssues(ctx context.Context, repoPath string, filter FilterConfig) ([]Issue, error) {
	pat := os.Getenv(asanaPATEnvVar)
	if pat == "" {
		return nil, fmt.Errorf("ASANA_PAT environment variable not set")
	}

	projectID := filter.Project
	if projectID == "" {
		return nil, fmt.Errorf("Asana project GID not configured for this repository")
	}
	// ... rest stays the same for now
```

**Step 6: Update LinearProvider.FetchIssues signature (no filtering logic yet)**

In `internal/issues/linear.go`, change the signature. Extract `projectID` from `filter.Team`:

```go
func (p *LinearProvider) FetchIssues(ctx context.Context, repoPath string, filter FilterConfig) ([]Issue, error) {
	apiKey := os.Getenv(linearAPIKeyEnvVar)
	if apiKey == "" {
		return nil, fmt.Errorf("LINEAR_API_KEY environment variable not set")
	}

	projectID := filter.Team
	if projectID == "" {
		return nil, fmt.Errorf("Linear team ID not configured for this repository")
	}
	// ... rest stays the same for now
```

**Step 7: Update all test call sites**

In `internal/issues/asana_test.go`, update every `p.FetchIssues(ctx, "/test/repo", "12345")` to `p.FetchIssues(ctx, "/test/repo", FilterConfig{Project: "12345"})`.

In `internal/issues/linear_test.go`, update every `p.FetchIssues(ctx, "/test/repo", "team-123")` to `p.FetchIssues(ctx, "/test/repo", FilterConfig{Team: "team-123"})`. Also update `p.FetchIssues(ctx, "/test/repo", "")` to `p.FetchIssues(ctx, "/test/repo", FilterConfig{})`.

**Step 8: Update daemon polling.go**

In `internal/daemon/polling.go`, update `fetchIssuesForProvider` to pass the full filter:

```go
case issues.SourceAsana, issues.SourceLinear:
	p := d.issueRegistry.GetProvider(provider)
	if p == nil {
		return nil, fmt.Errorf("provider %q not registered", provider)
	}
	return p.FetchIssues(ctx, repoPath, issues.FilterConfig{
		Label:   wfCfg.Source.Filter.Label,
		Project: wfCfg.Source.Filter.Project,
		Team:    wfCfg.Source.Filter.Team,
	})
```

**Step 9: Run tests to verify refactor compiles and passes**

Run: `go test -p=1 -count=1 ./internal/issues/... ./internal/daemon/...`
Expected: All existing tests PASS (no behavioral changes yet)

**Step 10: Commit**

```bash
git add internal/issues/provider.go internal/issues/provider_test.go internal/issues/github.go internal/issues/asana.go internal/issues/linear.go internal/issues/asana_test.go internal/issues/linear_test.go internal/daemon/polling.go
git commit -m "refactor: change FetchIssues to accept FilterConfig struct

Prepares for label/tag filtering by passing the full filter config
to providers instead of just the project/team ID."
```

---

### Task 2: Add Asana tag filtering

**Files:**
- Modify: `internal/issues/asana.go:66-131`
- Modify: `internal/issues/asana_test.go`

**Step 1: Write the failing test — tag filtering with matching tag**

Add to `internal/issues/asana_test.go`:

```go
func TestAsanaProvider_FetchIssues_TagFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify tags.name is in opt_fields
		optFields := r.URL.Query().Get("opt_fields")
		if !strings.Contains(optFields, "tags.name") {
			t.Errorf("expected opt_fields to contain 'tags.name', got %q", optFields)
		}

		response := asanaTasksResponse{
			Data: []asanaTask{
				{
					GID: "1", Name: "Tagged task", Notes: "Has the tag",
					Permalink: "https://app.asana.com/0/123/1",
					Tags: []asanaTag{{Name: "queued"}},
				},
				{
					GID: "2", Name: "Untagged task", Notes: "No matching tag",
					Permalink: "https://app.asana.com/0/123/2",
					Tags: []asanaTag{{Name: "other"}},
				},
				{
					GID: "3", Name: "No tags", Notes: "Empty tags",
					Permalink: "https://app.asana.com/0/123/3",
					Tags: nil,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	origPAT := os.Getenv(asanaPATEnvVar)
	defer os.Setenv(asanaPATEnvVar, origPAT)
	os.Setenv(asanaPATEnvVar, "test-pat")

	cfg := &config.Config{}
	p := NewAsanaProviderWithClient(cfg, server.Client(), server.URL)

	ctx := context.Background()
	issues, err := p.FetchIssues(ctx, "/test/repo", FilterConfig{Project: "12345", Label: "queued"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (filtered by tag), got %d", len(issues))
	}
	if issues[0].ID != "1" {
		t.Errorf("expected task GID '1', got %q", issues[0].ID)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test -p=1 -count=1 ./internal/issues/ -run TestAsanaProvider_FetchIssues_TagFilter -v`
Expected: FAIL — `asanaTag` undefined, `Tags` field missing from `asanaTask`

**Step 3: Write the failing test — case-insensitive match**

Add to `internal/issues/asana_test.go`:

```go
func TestAsanaProvider_FetchIssues_TagFilterCaseInsensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := asanaTasksResponse{
			Data: []asanaTask{
				{
					GID: "1", Name: "Mixed case", Notes: "",
					Permalink: "https://app.asana.com/0/123/1",
					Tags: []asanaTag{{Name: "Queued"}},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	origPAT := os.Getenv(asanaPATEnvVar)
	defer os.Setenv(asanaPATEnvVar, origPAT)
	os.Setenv(asanaPATEnvVar, "test-pat")

	cfg := &config.Config{}
	p := NewAsanaProviderWithClient(cfg, server.Client(), server.URL)

	ctx := context.Background()
	issues, err := p.FetchIssues(ctx, "/test/repo", FilterConfig{Project: "12345", Label: "queued"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (case-insensitive tag match), got %d", len(issues))
	}
}
```

**Step 4: Write the failing test — no label configured returns all tasks**

Add to `internal/issues/asana_test.go`:

```go
func TestAsanaProvider_FetchIssues_NoLabelReturnsAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := asanaTasksResponse{
			Data: []asanaTask{
				{GID: "1", Name: "Task 1", Notes: "", Permalink: "https://app.asana.com/0/123/1", Tags: []asanaTag{{Name: "queued"}}},
				{GID: "2", Name: "Task 2", Notes: "", Permalink: "https://app.asana.com/0/123/2", Tags: nil},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	origPAT := os.Getenv(asanaPATEnvVar)
	defer os.Setenv(asanaPATEnvVar, origPAT)
	os.Setenv(asanaPATEnvVar, "test-pat")

	cfg := &config.Config{}
	p := NewAsanaProviderWithClient(cfg, server.Client(), server.URL)

	ctx := context.Background()
	issues, err := p.FetchIssues(ctx, "/test/repo", FilterConfig{Project: "12345"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues (no label filter), got %d", len(issues))
	}
}
```

**Step 5: Implement Asana tag filtering**

In `internal/issues/asana.go`:

1. Add `asanaTag` struct and `Tags` field to `asanaTask`:

```go
type asanaTag struct {
	Name string `json:"name"`
}

type asanaTask struct {
	GID       string     `json:"gid"`
	Name      string     `json:"name"`
	Notes     string     `json:"notes"`
	Permalink string     `json:"permalink_url"`
	Tags      []asanaTag `json:"tags"`
}
```

2. Update the URL to include `tags.name` in `opt_fields`:

```go
url := fmt.Sprintf("%s/projects/%s/tasks?opt_fields=gid,name,notes,permalink_url,tags.name&completed_since=now", p.apiBase, projectID)
```

3. Add tag filtering after decoding the response (before building the issues slice):

```go
	tasks := tasksResp.Data

	// Filter by tag if label is configured
	if filter.Label != "" {
		var filtered []asanaTask
		for _, task := range tasks {
			for _, tag := range task.Tags {
				if strings.EqualFold(tag.Name, filter.Label) {
					filtered = append(filtered, task)
					break
				}
			}
		}
		tasks = filtered
	}

	issues := make([]Issue, len(tasks))
	for i, task := range tasks {
		// ... same as before
	}
```

Need to add `"strings"` to the import if not already there (it is — used in `GenerateBranchName`).

**Step 6: Run tests to verify they pass**

Run: `go test -p=1 -count=1 ./internal/issues/ -run TestAsanaProvider -v`
Expected: All Asana tests PASS

**Step 7: Commit**

```bash
git add internal/issues/asana.go internal/issues/asana_test.go
git commit -m "feat: add tag-based filtering for Asana provider

When source.filter.label is set, only Asana tasks with a matching
tag name are returned. Comparison is case-insensitive. When label
is empty, all incomplete tasks are returned (existing behavior)."
```

---

### Task 3: Add Linear label filtering

**Files:**
- Modify: `internal/issues/linear.go:107-185`
- Modify: `internal/issues/linear_test.go`

**Step 1: Write the failing test — label filter in GraphQL query**

Add to `internal/issues/linear_test.go`:

```go
func TestLinearProvider_FetchIssues_LabelFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var gqlReq linearGraphQLRequest
		json.Unmarshal(body, &gqlReq)

		// Verify the query contains label filter
		if !strings.Contains(gqlReq.Query, "labels") {
			t.Error("expected GraphQL query to contain label filter")
		}

		// Verify the label variable is passed
		if gqlReq.Variables["label"] != "queued" {
			t.Errorf("expected label variable 'queued', got %v", gqlReq.Variables["label"])
		}

		response := linearTeamIssuesResponse{}
		response.Data.Team.Issues.Nodes = []linearIssue{
			{ID: "uuid-1", Identifier: "ENG-123", Title: "Labeled issue", Description: "Has label", URL: "https://linear.app/team/issue/ENG-123"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	origKey := os.Getenv(linearAPIKeyEnvVar)
	defer os.Setenv(linearAPIKeyEnvVar, origKey)
	os.Setenv(linearAPIKeyEnvVar, "lin_api_test123")

	cfg := &config.Config{}
	p := NewLinearProviderWithClient(cfg, server.Client(), server.URL)

	ctx := context.Background()
	issues, err := p.FetchIssues(ctx, "/test/repo", FilterConfig{Team: "team-123", Label: "queued"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}
```

**Step 2: Write the failing test — no label omits label filter from query**

Add to `internal/issues/linear_test.go`:

```go
func TestLinearProvider_FetchIssues_NoLabelOmitsFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var gqlReq linearGraphQLRequest
		json.Unmarshal(body, &gqlReq)

		// Verify the query does NOT contain label filter
		if strings.Contains(gqlReq.Query, "labels") {
			t.Error("expected GraphQL query to NOT contain label filter when no label set")
		}

		response := linearTeamIssuesResponse{}
		response.Data.Team.Issues.Nodes = []linearIssue{
			{ID: "uuid-1", Identifier: "ENG-100", Title: "Issue 1", URL: "https://linear.app/issue/ENG-100"},
			{ID: "uuid-2", Identifier: "ENG-200", Title: "Issue 2", URL: "https://linear.app/issue/ENG-200"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	origKey := os.Getenv(linearAPIKeyEnvVar)
	defer os.Setenv(linearAPIKeyEnvVar, origKey)
	os.Setenv(linearAPIKeyEnvVar, "lin_api_test123")

	cfg := &config.Config{}
	p := NewLinearProviderWithClient(cfg, server.Client(), server.URL)

	ctx := context.Background()
	issues, err := p.FetchIssues(ctx, "/test/repo", FilterConfig{Team: "team-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}
}
```

**Step 3: Run tests to verify they fail**

Run: `go test -p=1 -count=1 ./internal/issues/ -run TestLinearProvider_FetchIssues_LabelFilter -v`
Expected: FAIL — query doesn't contain label filter, no label variable

**Step 4: Implement Linear label filtering**

In `internal/issues/linear.go`, update `FetchIssues` to conditionally include the label filter in the GraphQL query:

```go
	var query string
	variables := map[string]any{
		"teamId": filter.Team,
	}

	if filter.Label != "" {
		query = `query($teamId: String!, $label: String!) {
  team(id: $teamId) {
    issues(filter: {
      state: { type: { nin: ["completed", "canceled"] } }
      labels: { name: { eqIgnoreCase: $label } }
    }) {
      nodes {
        id
        identifier
        title
        description
        url
      }
    }
  }
}`
		variables["label"] = filter.Label
	} else {
		query = `query($teamId: String!) {
  team(id: $teamId) {
    issues(filter: { state: { type: { nin: ["completed", "canceled"] } } }) {
      nodes {
        id
        identifier
        title
        description
        url
      }
    }
  }
}`
	}
```

**Step 5: Run tests to verify they pass**

Run: `go test -p=1 -count=1 ./internal/issues/ -run TestLinearProvider -v`
Expected: All Linear tests PASS

**Step 6: Commit**

```bash
git add internal/issues/linear.go internal/issues/linear_test.go
git commit -m "feat: add label-based filtering for Linear provider

When source.filter.label is set, the GraphQL query includes a
labels filter using eqIgnoreCase. When label is empty, no label
filter is applied (existing behavior)."
```

---

### Task 4: Run full test suite

**Step 1: Run all tests**

Run: `go test -p=1 -count=1 ./...`
Expected: All tests PASS

**Step 2: Commit design doc**

```bash
git add docs/plans/2026-02-23-label-filtering-design.md
git commit -m "docs: add label filtering design document"
```
