package issues

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	linearAPIBase      = "https://api.linear.app"
	linearAPIKeyEnvVar = "LINEAR_API_KEY"
	linearHTTPTimeout  = 30 * time.Second
)

// LinearTeam represents a Linear team with its ID and name.
type LinearTeam struct {
	ID   string
	Name string
}

// LinearProvider implements Provider for Linear Issues using the Linear GraphQL API.
type LinearProvider struct {
	config     LinearConfigProvider
	httpClient *http.Client
	apiBase    string // Override for testing; defaults to linearAPIBase
}

// NewLinearProvider creates a new Linear issue provider.
func NewLinearProvider(cfg LinearConfigProvider) *LinearProvider {
	return &LinearProvider{
		config: cfg,
		httpClient: &http.Client{
			Timeout: linearHTTPTimeout,
		},
		apiBase: linearAPIBase,
	}
}

// NewLinearProviderWithClient creates a new Linear issue provider with a custom HTTP client and API base URL (for testing).
func NewLinearProviderWithClient(cfg LinearConfigProvider, client *http.Client, apiBase string) *LinearProvider {
	if apiBase == "" {
		apiBase = linearAPIBase
	}
	return &LinearProvider{
		config:     cfg,
		httpClient: client,
		apiBase:    apiBase,
	}
}

// Name returns the human-readable name of this provider.
func (p *LinearProvider) Name() string {
	return "Linear Issues"
}

// Source returns the source type for this provider.
func (p *LinearProvider) Source() Source {
	return SourceLinear
}

// linearGraphQLRequest represents a GraphQL request body.
type linearGraphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// linearIssue represents an issue from the Linear GraphQL API response.
type linearIssue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// linearTeamIssuesResponse represents the Linear GraphQL response for team issues.
type linearTeamIssuesResponse struct {
	Data struct {
		Team struct {
			Issues struct {
				Nodes []linearIssue `json:"nodes"`
			} `json:"issues"`
		} `json:"team"`
	} `json:"data"`
}

// linearTeam represents a team from the Linear GraphQL API response.
type linearTeam struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// linearTeamsResponse represents the Linear GraphQL response for listing teams.
type linearTeamsResponse struct {
	Data struct {
		Teams struct {
			Nodes []linearTeam `json:"nodes"`
		} `json:"teams"`
	} `json:"data"`
}

// FetchIssues retrieves active issues from the Linear team.
// The filter.Team should be the Linear team ID.
func (p *LinearProvider) FetchIssues(ctx context.Context, repoPath string, filter FilterConfig) ([]Issue, error) {
	apiKey := os.Getenv(linearAPIKeyEnvVar)
	if apiKey == "" {
		return nil, fmt.Errorf("LINEAR_API_KEY environment variable not set")
	}

	projectID := filter.Team
	if projectID == "" {
		return nil, fmt.Errorf("Linear team ID not configured for this repository")
	}

	var query string
	variables := map[string]any{
		"teamId": projectID,
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

	gqlReq := linearGraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	body, err := json.Marshal(gqlReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", p.apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issues: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("Linear API returned 403 Forbidden - check that your LINEAR_API_KEY has access to this team")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Linear API returned status %d", resp.StatusCode)
	}

	var gqlResp linearTeamIssuesResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse Linear response: %w", err)
	}

	nodes := gqlResp.Data.Team.Issues.Nodes
	issues := make([]Issue, len(nodes))
	for i, issue := range nodes {
		issues[i] = Issue{
			ID:     issue.Identifier,
			Title:  issue.Title,
			Body:   issue.Description,
			URL:    issue.URL,
			Source: SourceLinear,
		}
	}

	return issues, nil
}

// IsConfigured returns true if Linear is configured for the given repo.
// Requires both LINEAR_API_KEY env var and a team ID mapped to the repo.
func (p *LinearProvider) IsConfigured(repoPath string) bool {
	if os.Getenv(linearAPIKeyEnvVar) == "" {
		return false
	}
	return p.config.HasLinearTeam(repoPath)
}

// GenerateBranchName returns a branch name for the given Linear issue.
// Format: "linear-{identifier}" where identifier is lowercased (e.g., "linear-eng-123").
func (p *LinearProvider) GenerateBranchName(issue Issue) string {
	return fmt.Sprintf("linear-%s", strings.ToLower(issue.ID))
}

// GetPRLinkText returns the text to add to PR body to link/close the Linear issue.
// Linear supports auto-close via identifier mentions (e.g., "Fixes ENG-123").
func (p *LinearProvider) GetPRLinkText(issue Issue) string {
	return fmt.Sprintf("Fixes %s", issue.ID)
}

// RemoveLabel removes a label from a Linear issue using the GraphQL API.
// The issueID should be the issue's UUID (not the identifier like ENG-123).
// Implements ProviderActions.
func (p *LinearProvider) RemoveLabel(ctx context.Context, repoPath string, issueID string, label string) error {
	apiKey := os.Getenv(linearAPIKeyEnvVar)
	if apiKey == "" {
		return fmt.Errorf("LINEAR_API_KEY environment variable not set")
	}

	// First, find the label ID by name
	findLabelQuery := `query($label: String!) {
  issueLabels(filter: { name: { eqIgnoreCase: $label } }) {
    nodes { id name }
  }
}`
	findReq := linearGraphQLRequest{
		Query:     findLabelQuery,
		Variables: map[string]any{"label": label},
	}
	findBody, err := json.Marshal(findReq)
	if err != nil {
		return fmt.Errorf("failed to marshal label search request: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", p.apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(findBody))
	if err != nil {
		return fmt.Errorf("failed to create label search request: %w", err)
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to search for label: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Linear API returned status %d for label search", resp.StatusCode)
	}

	var labelResp struct {
		Data struct {
			IssueLabels struct {
				Nodes []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"issueLabels"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&labelResp); err != nil {
		return fmt.Errorf("failed to parse label search response: %w", err)
	}

	if len(labelResp.Data.IssueLabels.Nodes) == 0 {
		return fmt.Errorf("label %q not found in Linear", label)
	}
	labelID := labelResp.Data.IssueLabels.Nodes[0].ID

	// Remove the label from the issue
	removeMutation := `mutation($issueId: String!, $labelId: String!) {
  issueLabelDisconnect(id: $issueId, labelId: $labelId) {
    success
  }
}`
	removeReq := linearGraphQLRequest{
		Query:     removeMutation,
		Variables: map[string]any{"issueId": issueID, "labelId": labelID},
	}
	removeBody, err := json.Marshal(removeReq)
	if err != nil {
		return fmt.Errorf("failed to marshal label removal request: %w", err)
	}

	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(removeBody))
	if err != nil {
		return fmt.Errorf("failed to create label removal request: %w", err)
	}
	req2.Header.Set("Authorization", apiKey)
	req2.Header.Set("Content-Type", "application/json")

	resp2, err := p.httpClient.Do(req2)
	if err != nil {
		return fmt.Errorf("failed to remove label: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("Linear API returned status %d for label removal", resp2.StatusCode)
	}

	return nil
}

// Comment adds a comment to a Linear issue using the GraphQL API.
// The issueID should be the issue's UUID (not the identifier like ENG-123).
// Implements ProviderActions.
func (p *LinearProvider) Comment(ctx context.Context, repoPath string, issueID string, body string) error {
	apiKey := os.Getenv(linearAPIKeyEnvVar)
	if apiKey == "" {
		return fmt.Errorf("LINEAR_API_KEY environment variable not set")
	}

	mutation := `mutation($issueId: String!, $body: String!) {
  commentCreate(input: { issueId: $issueId, body: $body }) {
    success
  }
}`
	gqlReq := linearGraphQLRequest{
		Query:     mutation,
		Variables: map[string]any{"issueId": issueID, "body": body},
	}
	reqBody, err := json.Marshal(gqlReq)
	if err != nil {
		return fmt.Errorf("failed to marshal comment request: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", p.apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create comment request: %w", err)
	}
	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Linear API returned status %d for comment creation", resp.StatusCode)
	}

	return nil
}

// FetchTeams retrieves all teams accessible to the user.
func (p *LinearProvider) FetchTeams(ctx context.Context) ([]LinearTeam, error) {
	apiKey := os.Getenv(linearAPIKeyEnvVar)
	if apiKey == "" {
		return nil, fmt.Errorf("LINEAR_API_KEY environment variable not set")
	}

	gqlReq := linearGraphQLRequest{
		Query: `{ teams { nodes { id name } } }`,
	}

	body, err := json.Marshal(gqlReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	url := fmt.Sprintf("%s/graphql", p.apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch teams: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Linear API returned status %d", resp.StatusCode)
	}

	var gqlResp linearTeamsResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse Linear teams response: %w", err)
	}

	nodes := gqlResp.Data.Teams.Nodes
	teams := make([]LinearTeam, len(nodes))
	for i, team := range nodes {
		teams[i] = LinearTeam{
			ID:   team.ID,
			Name: team.Name,
		}
	}

	return teams, nil
}
