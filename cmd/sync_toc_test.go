package cmd

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/github/gh-stack/internal/config"
	"github.com/github/gh-stack/internal/git"
	"github.com/github/gh-stack/internal/github"
	"github.com/github/gh-stack/internal/stack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- generateStackTOC unit tests ---

func TestGenerateStackTOC_SinglePR_Empty(t *testing.T) {
	s := &stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 10, URL: "https://github.com/org/repo/pull/10"}},
		},
	}
	assert.Equal(t, "", generateStackTOC(s, 10, nil), "single PR should produce no TOC")
}

func TestGenerateStackTOC_NoPRs_Empty(t *testing.T) {
	s := &stack.Stack{
		Trunk:    stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{{Branch: "b1"}, {Branch: "b2"}},
	}
	assert.Equal(t, "", generateStackTOC(s, 0, nil), "branches without PRs should produce no TOC")
}

func TestGenerateStackTOC_SkipsMergedAndQueued(t *testing.T) {
	s := &stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 1, Merged: true}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 2, URL: "https://github.com/org/repo/pull/2"}},
		},
	}
	toc := generateStackTOC(s, 2, nil)
	assert.Equal(t, "", toc, "a single open PR (merged sibling skipped) should produce no TOC")
}

func TestGenerateStackTOC_TwoPRs_HasArrow(t *testing.T) {
	s := &stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 10, URL: "https://github.com/org/repo/pull/10"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 20, URL: "https://github.com/org/repo/pull/20"}},
		},
	}

	toc := generateStackTOC(s, 20, nil)

	assert.Contains(t, toc, stackTOCHeader, "TOC should start with sentinel comment")
	assert.Contains(t, toc, "<!-- gh-stack-toc-end -->", "TOC should end with closing sentinel")
	assert.Contains(t, toc, "#10", "should mention first PR")
	assert.Contains(t, toc, "#20", "should mention second PR")
	assert.Contains(t, toc, "you are here", "current PR should have arrow marker")

	// Arrow only on the current PR
	lines := strings.Split(toc, "\n")
	arrowCount := 0
	for _, l := range lines {
		if strings.Contains(l, "you are here") {
			arrowCount++
		}
	}
	assert.Equal(t, 1, arrowCount, "arrow should appear exactly once")
}

func TestGenerateStackTOC_OrderPreserved(t *testing.T) {
	s := &stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 1, URL: "https://github.com/org/repo/pull/1"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 2, URL: "https://github.com/org/repo/pull/2"}},
			{Branch: "b3", PullRequest: &stack.PullRequestRef{Number: 3, URL: "https://github.com/org/repo/pull/3"}},
		},
	}
	toc := generateStackTOC(s, 1, nil)

	idx1 := strings.Index(toc, "#1")
	idx2 := strings.Index(toc, "#2")
	idx3 := strings.Index(toc, "#3")
	assert.True(t, idx1 < idx2 && idx2 < idx3, "entries should be listed bottom-to-top (b1 first)")
}

func TestGenerateStackTOC_UsesTitleWhenAvailable(t *testing.T) {
	s := &stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 10, URL: "https://github.com/org/repo/pull/10"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 20, URL: "https://github.com/org/repo/pull/20"}},
		},
	}
	titles := map[int]string{10: "Add login page", 20: "Add logout page"}
	toc := generateStackTOC(s, 10, titles)

	assert.Contains(t, toc, "Add login page", "should show title for PR #10")
	assert.Contains(t, toc, "Add logout page", "should show title for PR #20")
	assert.NotContains(t, toc, "`b1`", "should not show branch name when title is available")
	assert.NotContains(t, toc, "`b2`", "should not show branch name when title is available")
}

func TestGenerateStackTOC_FallsBackToBranchNameWithoutTitle(t *testing.T) {
	s := &stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 10, URL: "https://github.com/org/repo/pull/10"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 20, URL: "https://github.com/org/repo/pull/20"}},
		},
	}
	// Only title for #10; #20 should fall back to branch name.
	titles := map[int]string{10: "Add login page"}
	toc := generateStackTOC(s, 10, titles)

	assert.Contains(t, toc, "Add login page", "should show title for PR #10")
	assert.Contains(t, toc, "b2", "should fall back to branch name for PR #20")
}

func TestStripStackTOC_NoBlock(t *testing.T) {
	body := "some existing PR description"
	assert.Equal(t, body, stripStackTOC(body))
}

func TestStripStackTOC_RemovesBlock(t *testing.T) {
	toc := "<!-- gh-stack-toc -->\n**Stack**\n- #1\n<!-- gh-stack-toc-end -->"
	body := toc + "\n\nActual PR content."
	got := stripStackTOC(body)
	assert.Equal(t, "Actual PR content.", got)
}

func TestStripStackTOC_EmptyBodyAfterStrip(t *testing.T) {
	toc := "<!-- gh-stack-toc -->\n**Stack**\n- #1\n<!-- gh-stack-toc-end -->"
	got := stripStackTOC(toc)
	assert.Equal(t, "", got)
}

func TestStripStackTOC_RoundTrip(t *testing.T) {
	s := &stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 1, URL: "https://github.com/org/repo/pull/1"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 2, URL: "https://github.com/org/repo/pull/2"}},
		},
	}
	original := "My PR description."
	toc := generateStackTOC(s, 1, nil)
	withTOC := toc + "\n\n" + original
	stripped := stripStackTOC(withTOC)
	assert.Equal(t, original, stripped, "strip should recover the original body exactly")
}

// --- updatePRBodiesWithTOC integration tests (via runSync) ---

func TestSync_TOC_WrittenToOpenPRs(t *testing.T) {
	s := stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 101, URL: "https://github.com/org/repo/pull/101"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 102, URL: "https://github.com/org/repo/pull/102"}},
		},
	}
	tmpDir := t.TempDir()
	writeStackFile(t, tmpDir, s)

	prTitles := map[int]string{101: "Add login page", 102: "Add logout page"}
	var mu sync.Mutex
	bodies := map[int]string{}
	ghMock := &github.MockClient{
		FindPRForBranchFn: openPRFinder(map[string]int{"b1": 101, "b2": 102}),
		FindPRByNumberFn: func(n int) (*github.PullRequest, error) {
			return &github.PullRequest{Number: n, Title: prTitles[n], Body: "existing body"}, nil
		},
		UpdatePRBodyFn: func(n int, body string) error {
			mu.Lock()
			bodies[n] = body
			mu.Unlock()
			return nil
		},
		ListStacksFn: func() ([]github.RemoteStack, error) { return nil, nil },
		CreateStackFn: func([]int) (*github.RemoteStack, error) {
			return &github.RemoteStack{ID: 1, Number: 1}, nil
		},
	}

	restore := git.SetOps(newSyncMockNoRebase(tmpDir, "b1"))
	defer restore()

	cfg, _, errR := config.NewTestConfig()
	cfg.GitHubClientOverride = ghMock
	cmd := SyncCmd(cfg)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	cfg.Err.Close()
	out, _ := io.ReadAll(errR)
	_ = out

	require.Contains(t, bodies, 101, "body for PR #101 should have been updated")
	require.Contains(t, bodies, 102, "body for PR #102 should have been updated")

	assert.Contains(t, bodies[101], stackTOCHeader)
	assert.Contains(t, bodies[101], "#101")
	assert.Contains(t, bodies[101], "#102")
	assert.Contains(t, bodies[101], "Add login page", "TOC for PR #101 should include its own title")
	assert.Contains(t, bodies[101], "Add logout page", "TOC for PR #101 should include the other PR's title")
	assert.Contains(t, bodies[101], "you are here", "PR #101 should be marked as current in its own TOC")

	assert.Contains(t, bodies[102], stackTOCHeader)
	assert.Contains(t, bodies[102], "#101")
	assert.Contains(t, bodies[102], "#102")
	assert.Contains(t, bodies[102], "Add login page", "TOC for PR #102 should include the other PR's title")
	assert.Contains(t, bodies[102], "Add logout page", "TOC for PR #102 should include its own title")
	assert.Contains(t, bodies[102], "you are here", "PR #102 should be marked as current in its own TOC")
}

func TestSync_TOC_SinglePR_NoUpdate(t *testing.T) {
	s := stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 101, URL: "https://github.com/org/repo/pull/101"}},
		},
	}
	tmpDir := t.TempDir()
	writeStackFile(t, tmpDir, s)

	var updateCalled bool
	ghMock := &github.MockClient{
		FindPRForBranchFn: openPRFinder(map[string]int{"b1": 101}),
		FindPRByNumberFn: func(n int) (*github.PullRequest, error) {
			return &github.PullRequest{Number: n, Body: "some body"}, nil
		},
		UpdatePRBodyFn: func(int, string) error {
			updateCalled = true
			return nil
		},
	}

	restore := git.SetOps(newSyncMockNoRebase(tmpDir, "b1"))
	defer restore()

	cfg, _, _ := config.NewTestConfig()
	cfg.GitHubClientOverride = ghMock
	cmd := SyncCmd(cfg)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	_ = cmd.Execute()

	assert.False(t, updateCalled, "UpdatePRBody should not be called for a single-PR stack")
}

func TestSync_TOC_SkipsMergedPRs(t *testing.T) {
	s := stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 101, Merged: true}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 102, URL: "https://github.com/org/repo/pull/102"}},
		},
	}
	tmpDir := t.TempDir()
	writeStackFile(t, tmpDir, s)

	var updateCalled bool
	ghMock := &github.MockClient{
		FindPRByNumberFn: func(n int) (*github.PullRequest, error) {
			return &github.PullRequest{Number: n, Body: "body"}, nil
		},
		FindPRForBranchFn: func(branch string) (*github.PullRequest, error) {
			if branch == "b2" {
				return &github.PullRequest{Number: 102, URL: "https://github.com/org/repo/pull/102"}, nil
			}
			return nil, nil
		},
		UpdatePRBodyFn: func(int, string) error {
			updateCalled = true
			return nil
		},
	}

	restore := git.SetOps(newSyncMockNoRebase(tmpDir, "b2"))
	defer restore()

	cfg, _, _ := config.NewTestConfig()
	cfg.GitHubClientOverride = ghMock
	cmd := SyncCmd(cfg)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	_ = cmd.Execute()

	// Only one open PR after filtering merged — no TOC written
	assert.False(t, updateCalled, "UpdatePRBody should not be called when only one PR is open")
}

func TestSync_TOC_PreservesExistingBody(t *testing.T) {
	s := stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 101, URL: "https://github.com/org/repo/pull/101"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 102, URL: "https://github.com/org/repo/pull/102"}},
		},
	}
	tmpDir := t.TempDir()
	writeStackFile(t, tmpDir, s)

	existingBody := "This is my meaningful PR description."
	var mu sync.Mutex
	bodies := map[int]string{}
	ghMock := &github.MockClient{
		FindPRForBranchFn: openPRFinder(map[string]int{"b1": 101, "b2": 102}),
		FindPRByNumberFn: func(n int) (*github.PullRequest, error) {
			return &github.PullRequest{Number: n, Body: existingBody}, nil
		},
		UpdatePRBodyFn: func(n int, body string) error {
			mu.Lock()
			bodies[n] = body
			mu.Unlock()
			return nil
		},
		ListStacksFn:  func() ([]github.RemoteStack, error) { return nil, nil },
		CreateStackFn: func([]int) (*github.RemoteStack, error) { return &github.RemoteStack{ID: 1, Number: 1}, nil },
	}

	restore := git.SetOps(newSyncMockNoRebase(tmpDir, "b1"))
	defer restore()

	cfg, _, _ := config.NewTestConfig()
	cfg.GitHubClientOverride = ghMock
	cmd := SyncCmd(cfg)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	for n, body := range bodies {
		assert.Contains(t, body, existingBody,
			"PR #%d body should still contain the original description", n)
		assert.Contains(t, body, stackTOCHeader,
			"PR #%d body should contain the TOC header", n)
	}
}

func TestSync_TOC_ReplacesExistingTOC(t *testing.T) {
	s := stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1", PullRequest: &stack.PullRequestRef{Number: 101, URL: "https://github.com/org/repo/pull/101"}},
			{Branch: "b2", PullRequest: &stack.PullRequestRef{Number: 102, URL: "https://github.com/org/repo/pull/102"}},
		},
	}
	tmpDir := t.TempDir()
	writeStackFile(t, tmpDir, s)

	oldTOC := "<!-- gh-stack-toc -->\n**Stack**\n- #99 (`old-branch`)\n<!-- gh-stack-toc-end -->"
	existingBody := oldTOC + "\n\nMy description."
	var mu sync.Mutex
	bodies := map[int]string{}
	ghMock := &github.MockClient{
		FindPRForBranchFn: openPRFinder(map[string]int{"b1": 101, "b2": 102}),
		FindPRByNumberFn: func(n int) (*github.PullRequest, error) {
			return &github.PullRequest{Number: n, Body: existingBody}, nil
		},
		UpdatePRBodyFn: func(n int, body string) error {
			mu.Lock()
			bodies[n] = body
			mu.Unlock()
			return nil
		},
		ListStacksFn:  func() ([]github.RemoteStack, error) { return nil, nil },
		CreateStackFn: func([]int) (*github.RemoteStack, error) { return &github.RemoteStack{ID: 1, Number: 1}, nil },
	}

	restore := git.SetOps(newSyncMockNoRebase(tmpDir, "b1"))
	defer restore()

	cfg, _, _ := config.NewTestConfig()
	cfg.GitHubClientOverride = ghMock
	cmd := SyncCmd(cfg)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	for _, body := range bodies {
		// Old stale entry must be gone; new ones present
		assert.NotContains(t, body, "#99", "stale TOC entry should have been replaced")
		assert.Contains(t, body, "#101")
		assert.Contains(t, body, "#102")
		assert.Contains(t, body, "My description.", "original PR description should be preserved")
	}
}

// --- Submit path TOC tests ---

func TestSubmit_TOC_WrittenToNewlyCreatedPRs(t *testing.T) {
	s := stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1"},
			{Branch: "b2"},
		},
	}
	tmpDir := t.TempDir()
	writeStackFile(t, tmpDir, s)

	mock := newSubmitMock(tmpDir, "b1")
	restoreGit := git.SetOps(mock)
	defer restoreGit()

	prCounter := 200
	var mu sync.Mutex
	bodies := map[int]string{}

	ghMock := &github.MockClient{
		FindPRForBranchFn: func(string) (*github.PullRequest, error) { return nil, nil },
		CreatePRFn: func(base, head, title, body string, draft bool) (*github.PullRequest, error) {
			mu.Lock()
			prCounter++
			n := prCounter
			mu.Unlock()
			return &github.PullRequest{
				Number: n,
				ID:     fmt.Sprintf("PR_%d", n),
				URL:    fmt.Sprintf("https://github.com/org/repo/pull/%d", n),
			}, nil
		},
		FindPRByNumberFn: func(n int) (*github.PullRequest, error) {
			return &github.PullRequest{Number: n, Body: ""}, nil
		},
		UpdatePRBodyFn: func(n int, body string) error {
			mu.Lock()
			bodies[n] = body
			mu.Unlock()
			return nil
		},
		ListStacksFn:  func() ([]github.RemoteStack, error) { return nil, nil },
		CreateStackFn: func([]int) (*github.RemoteStack, error) { return &github.RemoteStack{ID: 1, Number: 1}, nil },
	}

	cfg, _, _ := config.NewTestConfig()
	cfg.GitHubClientOverride = ghMock
	cmd := SubmitCmd(cfg)
	cmd.SetArgs([]string{"--auto"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	require.Len(t, bodies, 2, "both newly created PRs should have received a TOC update")
	for n, body := range bodies {
		assert.Contains(t, body, stackTOCHeader, "PR #%d body should contain TOC header", n)
		assert.Contains(t, body, "you are here", "PR #%d should have the current-PR marker", n)
	}
}

func TestSubmit_TOC_SingleNewPR_NoUpdate(t *testing.T) {
	s := stack.Stack{
		Trunk: stack.BranchRef{Branch: "main"},
		Branches: []stack.BranchRef{
			{Branch: "b1"},
		},
	}
	tmpDir := t.TempDir()
	writeStackFile(t, tmpDir, s)

	mock := newSubmitMock(tmpDir, "b1")
	restoreGit := git.SetOps(mock)
	defer restoreGit()

	var updateCalled bool
	ghMock := &github.MockClient{
		FindPRForBranchFn: func(string) (*github.PullRequest, error) { return nil, nil },
		CreatePRFn: func(base, head, title, body string, draft bool) (*github.PullRequest, error) {
			return &github.PullRequest{Number: 201, ID: "PR_201", URL: "https://github.com/org/repo/pull/201"}, nil
		},
		FindPRByNumberFn: func(n int) (*github.PullRequest, error) {
			return &github.PullRequest{Number: n, Body: ""}, nil
		},
		UpdatePRBodyFn: func(int, string) error {
			updateCalled = true
			return nil
		},
	}

	cfg, _, _ := config.NewTestConfig()
	cfg.GitHubClientOverride = ghMock
	cmd := SubmitCmd(cfg)
	cmd.SetArgs([]string{"--auto"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	assert.False(t, updateCalled, "UpdatePRBody should not be called for a single-PR stack")
}
