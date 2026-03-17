package issues

import (
	"context"
	"fmt"
	"sync"
)

// Compile-time interface checks.
var (
	_ Provider               = (*FakeProvider)(nil)
	_ ProviderActions        = (*FakeProvider)(nil)
	_ ProviderGateChecker    = (*FakeProvider)(nil)
	_ ProviderClaimManager   = (*FakeProvider)(nil)
	_ ProviderCommentUpdater = (*FakeProvider)(nil)
	_ IssueGetter            = (*FakeProvider)(nil)
	_ IssueStateChecker      = (*FakeProvider)(nil)
	_ ProviderSectionChecker = (*FakeProvider)(nil)
	_ ProviderSectionMover   = (*FakeProvider)(nil)
)

// FakeProviderCall records a single method invocation on FakeProvider.
type FakeProviderCall struct {
	IssueID string
	Args    []string // label, body, section, commentID, etc.
}

// FakeProvider is a controllable test double implementing all provider interfaces.
type FakeProvider struct {
	mu         sync.Mutex
	source     Source
	configured bool
	issues     []Issue
	fetchErr   error

	// Per-issue data
	comments     map[string][]IssueComment  // issueID → comments
	labels       map[string]map[string]bool // issueID → label set
	closedIssues map[string]bool            // issueID → closed
	claims       map[string][]ClaimInfo     // issueID → claims
	sections     map[string]string          // issueID → section name
	issuesByID   map[string]Issue           // issueID → issue

	// Call recording (for assertions)
	CommentCalls       []FakeProviderCall
	RemoveLabelCalls   []FakeProviderCall
	PostClaimCalls     []FakeProviderCall
	DeleteClaimCalls   []FakeProviderCall
	MoveToSectionCalls []FakeProviderCall
	UpdateCommentCalls []FakeProviderCall
}

// NewFakeProvider creates a new FakeProvider with the given source.
// Defaults: configured=true, empty issues/comments/labels.
func NewFakeProvider(source Source) *FakeProvider {
	return &FakeProvider{
		source:       source,
		configured:   true,
		comments:     make(map[string][]IssueComment),
		labels:       make(map[string]map[string]bool),
		closedIssues: make(map[string]bool),
		claims:       make(map[string][]ClaimInfo),
		sections:     make(map[string]string),
		issuesByID:   make(map[string]Issue),
	}
}

// --- Control methods ---

// SetIssues sets what FetchIssues returns.
func (f *FakeProvider) SetIssues(issues []Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issues = issues
	for _, issue := range issues {
		f.issuesByID[issue.ID] = issue
	}
}

// SetFetchError makes FetchIssues return an error.
func (f *FakeProvider) SetFetchError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetchErr = err
}

// SetComments sets what GetIssueComments returns for the given issue.
func (f *FakeProvider) SetComments(issueID string, comments []IssueComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[issueID] = comments
}

// AddLabel adds a label to an issue's label set.
func (f *FakeProvider) AddLabel(issueID, label string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.labels[issueID] == nil {
		f.labels[issueID] = make(map[string]bool)
	}
	f.labels[issueID][label] = true
}

// SetIssueClosed marks an issue as closed or open.
func (f *FakeProvider) SetIssueClosed(issueID string, closed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closedIssues[issueID] = closed
}

// SetSection sets the current section for an issue.
func (f *FakeProvider) SetSection(issueID, section string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sections[issueID] = section
}

// SetConfigured sets whether this provider reports as configured.
func (f *FakeProvider) SetConfigured(configured bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configured = configured
}

// AddIssue adds a single issue to the issuesByID map (for GetIssue).
func (f *FakeProvider) AddIssue(issue Issue) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issuesByID[issue.ID] = issue
}

// --- Provider interface (6 methods) ---

func (f *FakeProvider) Name() string {
	return fmt.Sprintf("Fake-%s", f.source)
}

func (f *FakeProvider) Source() Source {
	return f.source
}

func (f *FakeProvider) FetchIssues(_ context.Context, _ string, _ FilterConfig) ([]Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	result := make([]Issue, len(f.issues))
	copy(result, f.issues)
	return result, nil
}

func (f *FakeProvider) IsConfigured(_ string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configured
}

func (f *FakeProvider) GenerateBranchName(issue Issue) string {
	return fmt.Sprintf("issue-%s", issue.ID)
}

func (f *FakeProvider) GetPRLinkText(issue Issue) string {
	return fmt.Sprintf("Fixes #%s", issue.ID)
}

// --- ProviderActions ---

func (f *FakeProvider) RemoveLabel(_ context.Context, _ string, issueID string, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RemoveLabelCalls = append(f.RemoveLabelCalls, FakeProviderCall{
		IssueID: issueID,
		Args:    []string{label},
	})
	if ls, ok := f.labels[issueID]; ok {
		delete(ls, label)
	}
	return nil
}

func (f *FakeProvider) Comment(_ context.Context, _ string, issueID string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CommentCalls = append(f.CommentCalls, FakeProviderCall{
		IssueID: issueID,
		Args:    []string{body},
	})
	return nil
}

// --- ProviderCommentUpdater ---

func (f *FakeProvider) UpdateComment(_ context.Context, _ string, issueID string, commentID string, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpdateCommentCalls = append(f.UpdateCommentCalls, FakeProviderCall{
		IssueID: issueID,
		Args:    []string{commentID, body},
	})
	return nil
}

// --- ProviderGateChecker ---

func (f *FakeProvider) CheckIssueHasLabel(_ context.Context, _ string, issueID string, label string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ls, ok := f.labels[issueID]; ok {
		return ls[label], nil
	}
	return false, nil
}

func (f *FakeProvider) GetIssueComments(_ context.Context, _ string, issueID string) ([]IssueComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.comments[issueID], nil
}

// --- ProviderClaimManager ---

func (f *FakeProvider) PostClaim(_ context.Context, _ string, issueID string, claim ClaimInfo) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PostClaimCalls = append(f.PostClaimCalls, FakeProviderCall{
		IssueID: issueID,
		Args:    []string{claim.DaemonID},
	})
	commentID := fmt.Sprintf("claim-%s-%d", issueID, len(f.claims[issueID]))
	f.claims[issueID] = append(f.claims[issueID], claim)
	return commentID, nil
}

func (f *FakeProvider) GetClaims(_ context.Context, _ string, issueID string) ([]ClaimInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims[issueID], nil
}

func (f *FakeProvider) DeleteClaim(_ context.Context, _ string, issueID string, commentID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeleteClaimCalls = append(f.DeleteClaimCalls, FakeProviderCall{
		IssueID: issueID,
		Args:    []string{commentID},
	})
	return nil
}

// --- IssueGetter ---

func (f *FakeProvider) GetIssue(_ context.Context, _ string, id string) (*Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if issue, ok := f.issuesByID[id]; ok {
		return &issue, nil
	}
	return nil, fmt.Errorf("issue %s not found", id)
}

// --- IssueStateChecker ---

func (f *FakeProvider) IsIssueClosed(_ context.Context, _ string, issueID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closedIssues[issueID], nil
}

// --- ProviderSectionChecker ---

func (f *FakeProvider) IsInSection(_ context.Context, _ string, issueID string, section string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sections[issueID] == section, nil
}

// --- ProviderSectionMover ---

func (f *FakeProvider) MoveToSection(_ context.Context, _ string, issueID string, section string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.MoveToSectionCalls = append(f.MoveToSectionCalls, FakeProviderCall{
		IssueID: issueID,
		Args:    []string{section},
	})
	f.sections[issueID] = section
	return nil
}
