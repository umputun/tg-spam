//go:build e2e

// Package e2e contains end-to-end tests for the tg-spam web UI.
// tests verify that pages load correctly and basic HTMX interactions work.
package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	baseURL      = "http://localhost:18090"
	testDBPath   = "/tmp/tg-spam-e2e.db"
	testDataPath = "/tmp/tg-spam-e2e-data"
	testPassword = "e2e-test-password"
)

var (
	pw        *playwright.Playwright
	browser   playwright.Browser
	serverCmd *exec.Cmd
)

func TestMain(m *testing.M) {
	// clean old test data
	_ = os.Remove(testDBPath)
	_ = os.RemoveAll(testDataPath)
	_ = os.MkdirAll(testDataPath, 0o755)

	// create minimal sample files required by the app (preset samples)
	if err := os.WriteFile(testDataPath+"/spam-samples.txt", []byte("buy crypto now\nfree money giveaway\n"), 0o644); err != nil {
		fmt.Printf("failed to create spam samples: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(testDataPath+"/ham-samples.txt", []byte("hello world\nnice to meet you\n"), 0o644); err != nil {
		fmt.Printf("failed to create ham samples: %v\n", err)
		os.Exit(1)
	}

	// build test binary from project root
	build := exec.Command("go", "build", "-o", "/tmp/tg-spam-e2e", "./app")
	build.Dir = ".." // run from project root
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Printf("failed to build: %v\n", err)
		os.Exit(1)
	}

	// start server in web-only mode (no telegram token needed)
	serverCmd = exec.Command("/tmp/tg-spam-e2e",
		"--server.enabled",
		"--server.listen=:18090",
		"--server.auth="+testPassword,
		"--db="+testDBPath,
		"--files.samples="+testDataPath,
		"--files.dynamic="+testDataPath,
		"--dbg",
	)
	serverCmd.Stdout = os.Stdout
	serverCmd.Stderr = os.Stderr
	if err := serverCmd.Start(); err != nil {
		fmt.Printf("failed to start server: %v\n", err)
		os.Exit(1)
	}

	// wait for server readiness
	if err := waitForServer(baseURL+"/ping", 30*time.Second); err != nil {
		fmt.Printf("server not ready: %v\n", err)
		_ = serverCmd.Process.Kill()
		os.Exit(1)
	}

	// install playwright browsers
	if err := playwright.Install(&playwright.RunOptions{
		Browsers: []string{"chromium"},
	}); err != nil {
		fmt.Printf("failed to install playwright: %v\n", err)
		_ = serverCmd.Process.Kill()
		os.Exit(1)
	}

	// start playwright
	var err error
	pw, err = playwright.Run()
	if err != nil {
		fmt.Printf("failed to start playwright: %v\n", err)
		_ = serverCmd.Process.Kill()
		os.Exit(1)
	}

	// launch browser once (reused across all tests via contexts)
	headless := os.Getenv("E2E_HEADLESS") != "false"
	var slowMo float64
	if !headless {
		slowMo = 50 // slow down visible browser for easier observation
	}
	browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(headless),
		SlowMo:   playwright.Float(slowMo),
	})
	if err != nil {
		fmt.Printf("failed to launch browser: %v\n", err)
		_ = pw.Stop()
		_ = serverCmd.Process.Kill()
		os.Exit(1)
	}

	// run tests
	code := m.Run()

	// cleanup
	_ = browser.Close()
	_ = pw.Stop()
	_ = serverCmd.Process.Kill()
	_ = os.Remove(testDBPath)
	_ = os.RemoveAll("/tmp/tg-spam-e2e-data")

	os.Exit(code)
}

func waitForServer(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // test url
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("server not ready after %v", timeout)
}

// newPage creates a new browser page with authentication.
// each test gets isolated browser context with fresh cookies.
func newPage(t *testing.T) playwright.Page {
	t.Helper()
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		HttpCredentials: &playwright.HttpCredentials{
			Username: "tg-spam",
			Password: testPassword,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctx.Close() })

	page, err := ctx.NewPage()
	require.NoError(t, err)
	return page
}

// waitVisible waits for locator to become visible
func waitVisible(t *testing.T, loc playwright.Locator) {
	t.Helper()
	require.NoError(t, loc.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}))
}

// --- page load tests ---

func TestChecker_PageLoads(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL)
	require.NoError(t, err)

	// verify page title
	title, err := page.Title()
	require.NoError(t, err)
	assert.Contains(t, title, "Checker")
	assert.Contains(t, title, "TG-Spam")

	// verify main elements
	waitVisible(t, page.Locator("h2:has-text('Message Checker')"))
	waitVisible(t, page.Locator("textarea[name='msg']"))
	waitVisible(t, page.Locator("button[type='submit']:has-text('Check')"))
}

func TestManageSamples_PageLoads(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_samples")
	require.NoError(t, err)

	title, err := page.Title()
	require.NoError(t, err)
	assert.Contains(t, title, "Manage Samples")

	// verify both spam and ham sections exist
	waitVisible(t, page.Locator("h2:has-text('Manage Samples')"))
	waitVisible(t, page.Locator("button:has-text('Add Spam')"))
	waitVisible(t, page.Locator("button:has-text('Add Ham')"))
}

func TestManageUsers_PageLoads(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_users")
	require.NoError(t, err)

	title, err := page.Title()
	require.NoError(t, err)
	assert.Contains(t, title, "Manage Users")

	waitVisible(t, page.Locator("h2:has-text('Manage Approved Users')"))
	waitVisible(t, page.Locator("input[name='user_id']"))
	waitVisible(t, page.Locator("input[name='user_name']"))
	waitVisible(t, page.Locator("button:has-text('Add to Approved Users')"))
}

func TestManageDictionary_PageLoads(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_dictionary")
	require.NoError(t, err)

	title, err := page.Title()
	require.NoError(t, err)
	assert.Contains(t, title, "Manage Dictionary")

	waitVisible(t, page.Locator("h2:has-text('Manage Dictionary')"))
}

func TestDetectedSpam_PageLoads(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/detected_spam")
	require.NoError(t, err)

	title, err := page.Title()
	require.NoError(t, err)
	assert.Contains(t, title, "Detected Spam")
}

func TestSettings_PageLoads(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/list_settings")
	require.NoError(t, err)

	title, err := page.Title()
	require.NoError(t, err)
	assert.Contains(t, title, "Settings")
}

// --- navigation tests ---

func TestNavbar_NavigationWorks(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL)
	require.NoError(t, err)

	// verify navbar is visible
	waitVisible(t, page.Locator(".navbar"))

	// test navigation links
	tests := []struct {
		linkText string
		urlPath  string
	}{
		{"Manage Samples", "/manage_samples"},
		{"Manage Users", "/manage_users"},
		{"Manage Dictionary", "/manage_dictionary"},
		{"Detected Spam", "/detected_spam"},
		{"Settings", "/list_settings"},
		{"Checker", "/"},
	}

	for _, tc := range tests {
		t.Run(tc.linkText, func(t *testing.T) {
			link := page.Locator(fmt.Sprintf(".nav-link:has-text('%s')", tc.linkText))
			require.NoError(t, link.Click())
			require.NoError(t, page.WaitForURL(baseURL+tc.urlPath))
		})
	}
}

// --- spam check tests ---

func TestChecker_CheckEmptyMessage(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL)
	require.NoError(t, err)

	// submit empty message
	require.NoError(t, page.Locator("button[type='submit']:has-text('Check')").Click())

	// wait for result - empty message should not be spam
	result := page.Locator("#result .alert")
	waitVisible(t, result)
}

func TestChecker_CheckMessage(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL)
	require.NoError(t, err)

	// fill in test message
	require.NoError(t, page.Locator("textarea[name='msg']").Fill("Hello, this is a test message"))
	require.NoError(t, page.Locator("input[name='user_id']").Fill("123456"))

	// submit
	require.NoError(t, page.Locator("button[type='submit']:has-text('Check')").Click())

	// wait for result container with alert-light (outer container)
	result := page.Locator("#result .alert-light")
	waitVisible(t, result)

	// check that result contains either "Spam detected" or "No spam detected"
	text, err := result.TextContent()
	require.NoError(t, err)
	assert.True(t, contains(text, "spam"), "result should mention spam status")
}

// --- samples management tests ---

func TestManageSamples_AddSpam(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_samples")
	require.NoError(t, err)

	// add spam sample
	sampleText := "e2e test spam sample " + time.Now().Format("150405")
	require.NoError(t, page.Locator("textarea[placeholder='Enter spam sample']").Fill(sampleText))
	require.NoError(t, page.Locator("button:has-text('Add Spam')").Click())

	// verify sample appears in list
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#spam-samples-list").First().TextContent()
		return e == nil && contains(text, sampleText)
	}, 5*time.Second, 100*time.Millisecond)
}

func TestManageSamples_AddHam(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_samples")
	require.NoError(t, err)

	// add ham sample
	sampleText := "e2e test ham sample " + time.Now().Format("150405")
	require.NoError(t, page.Locator("textarea[placeholder='Enter ham sample']").Fill(sampleText))
	require.NoError(t, page.Locator("button:has-text('Add Ham')").Click())

	// verify sample appears in list
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#ham-samples-list").First().TextContent()
		return e == nil && contains(text, sampleText)
	}, 5*time.Second, 100*time.Millisecond)
}

// --- users management tests ---

func TestManageUsers_AddAndDeleteUser(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_users")
	require.NoError(t, err)

	// add approved user
	userID := time.Now().Format("150405")
	userName := "e2e_test_user_" + userID
	require.NoError(t, page.Locator("input[name='user_id']").Fill(userID))
	require.NoError(t, page.Locator("input[name='user_name']").Fill(userName))
	require.NoError(t, page.Locator("button:has-text('Add to Approved Users')").Click())

	// verify user appears in table
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#users-list table").First().TextContent()
		return e == nil && contains(text, userName)
	}, 5*time.Second, 100*time.Millisecond)

	// delete the user - find row with our user and click delete
	deleteBtn := page.Locator(fmt.Sprintf("tr:has-text('%s') button.btn-danger", userName))
	require.NoError(t, deleteBtn.Click())

	// verify user is removed
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#users-list table").First().TextContent()
		return e == nil && !contains(text, userName)
	}, 5*time.Second, 100*time.Millisecond)
}

// --- dictionary management tests ---

func TestManageDictionary_AddStopPhrase(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_dictionary")
	require.NoError(t, err)

	// add stop phrase
	phrase := "e2e_stop_phrase_" + time.Now().Format("150405")
	require.NoError(t, page.Locator("textarea[placeholder='Enter stop phrase']").Fill(phrase))
	require.NoError(t, page.Locator("button:has-text('Add Stop Phrase')").Click())

	// verify phrase appears in list
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#stop-phrases-list").First().TextContent()
		return e == nil && contains(text, phrase)
	}, 5*time.Second, 100*time.Millisecond)
}

func TestManageDictionary_AddIgnoredWord(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_dictionary")
	require.NoError(t, err)

	// add ignored word
	word := "e2eignored" + time.Now().Format("150405")
	require.NoError(t, page.Locator("textarea[placeholder='Enter ignored word']").Fill(word))
	require.NoError(t, page.Locator("button:has-text('Add Ignored Word')").Click())

	// verify word appears in list
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#ignored-words-list").First().TextContent()
		return e == nil && contains(text, word)
	}, 5*time.Second, 100*time.Millisecond)
}

// --- samples delete tests ---

func TestManageSamples_DeleteSpam(t *testing.T) {
	page := newPage(t)
	_, err := page.Goto(baseURL + "/manage_samples")
	require.NoError(t, err)

	// first add a spam sample
	sampleText := "e2e delete spam " + time.Now().Format("150405")
	require.NoError(t, page.Locator("textarea[placeholder='Enter spam sample']").Fill(sampleText))
	require.NoError(t, page.Locator("button:has-text('Add Spam')").Click())

	// verify it was added
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#spam-samples-list").First().TextContent()
		return e == nil && contains(text, sampleText)
	}, 5*time.Second, 100*time.Millisecond)

	// delete the sample - find row with our sample and click delete
	deleteBtn := page.Locator(fmt.Sprintf("#spam-samples-list li:has-text('%s') button.btn-danger", sampleText))
	require.NoError(t, deleteBtn.Click())

	// verify sample is removed
	assert.Eventually(t, func() bool {
		text, e := page.Locator("#spam-samples-list").First().TextContent()
		return e == nil && !contains(text, sampleText)
	}, 5*time.Second, 100*time.Millisecond)
}

// helper function to check if string contains substring (case insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsIgnoreCase(s, substr)))
}

func containsIgnoreCase(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalFoldAt(s, i, substr) {
			return true
		}
	}
	return false
}

func equalFoldAt(s string, i int, substr string) bool {
	for j := 0; j < len(substr); j++ {
		c1, c2 := s[i+j], substr[j]
		if c1 == c2 {
			continue
		}
		if 'A' <= c1 && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if 'A' <= c2 && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return false
		}
	}
	return true
}
