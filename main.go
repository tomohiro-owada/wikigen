package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── Progress ──

type Progress struct {
	mu         sync.Mutex
	totalItems int
	doneItems  int32
	current    map[string]string
}

func newProgress(total int) *Progress {
	return &Progress{totalItems: total, current: make(map[string]string)}
}

func (p *Progress) set(name, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current[name] = status
	p.print()
}

func (p *Progress) done(name string) {
	atomic.AddInt32(&p.doneItems, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.current, name)
	p.print()
}

func (p *Progress) print() {
	done := int(atomic.LoadInt32(&p.doneItems))
	fmt.Fprintf(os.Stderr, "\r\033[K[%d/%d] ", done, p.totalItems)
	var parts []string
	for name, status := range p.current {
		parts = append(parts, fmt.Sprintf("%s: %s", name, status))
	}
	if len(parts) > 0 {
		fmt.Fprintf(os.Stderr, "%s", strings.Join(parts, " | "))
	}
}

// ── Wiki Page ──

type WikiPage struct {
	ID          string
	Title       string
	Filename    string
	Description string
	Content     string
}

// ── Repo Entry ──

type RepoEntry struct {
	Project string // group name (empty = standalone)
	Repo    string // owner/repo
}

// ── Claude CLI ──

func claudeCall(claudePath, model string, repoDirs []string, systemPrompt, prompt string) (string, error) {
	args := []string{"-p", "--output-format", "text", "--dangerously-skip-permissions"}
	if model != "" {
		args = append(args, "--model", model)
	}
	for _, dir := range repoDirs {
		args = append(args, "--add-dir", dir)
	}
	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	cmd := exec.Command(claudePath, args...)
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude: %v\nstderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// ── Git Clone ──

func getGHToken() string {
	cmd := exec.Command("gh", "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitClone(repoURL, token, destDir string, useGH bool) error {
	if _, err := os.Stat(filepath.Join(destDir, ".git")); err == nil {
		cmd := exec.Command("git", "-C", destDir, "pull", "--ff-only")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// If using gh, get token from gh auth and clone with git
	if useGH && token == "" {
		token = getGHToken()
		if token == "" {
			return fmt.Errorf("gh auth token failed. Run: gh auth login")
		}
	}

	cloneURL := repoURL
	if token != "" {
		cloneURL = strings.Replace(repoURL, "https://", fmt.Sprintf("https://%s@", token), 1)
	}

	cmd := exec.Command("git", "clone", "--depth=1", "--single-branch", cloneURL, destDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ── Prompts ──

const xmlSystemPrompt = `CRITICAL INSTRUCTIONS FOR XML RESPONSES:
When the user requests XML output (e.g., wiki_structure, or any XML format):
1. Return ONLY the raw XML - no markdown code fences, no backticks, no explanation
2. Do NOT wrap XML in triple backticks or markdown code blocks
3. Do NOT add any text before or after the XML
4. Start directly with the opening XML tag and end with the closing XML tag
5. Ensure the XML is well-formed and valid`

func structurePrompt(projectName string, repos []string, language string) string {
	langName := languageName(language)
	repoList := strings.Join(repos, ", ")

	return fmt.Sprintf(`You are an expert software architect and technical writer.
Analyze the following repositories and design a comprehensive wiki structure.

Project: %s
Repositories: %s

You have full access to ALL repository source code via the tools available to you.
USE the Read, Grep, Glob, and Bash tools to thoroughly examine the codebase before designing the structure.

Your task is to determine what documentation pages are needed based on WHAT ACTUALLY EXISTS in the codebase.
Do NOT create pages for things that don't exist in the repositories.

IMPORTANT: All content will be written in %s.

## Documentation Categories

Analyze the repositories and create pages from the following categories AS APPLICABLE:

### A. Core Documentation (from code — factual)
- **System Overview**: Project purpose, tech stack, directory structure
- **Architecture**: Overall system design, component relationships, design patterns
- **API Specification**: REST/GraphQL endpoints, request/response schemas, authentication
- **Data Model**: Database schema, migrations, ORM models, ER diagrams
- **Routing & Navigation**: URL structure, page routing, middleware chain
- **State Management**: Store design, data flow, state patterns
- **Component Catalog**: UI components, atomic design structure, props/events
- **Configuration & Environment**: Config files, environment variables, feature flags
- **Build & Deployment**: Dockerfile, CI/CD pipelines, build scripts, infrastructure
- **Testing Strategy**: Test structure, test utilities, coverage approach
- **Authentication & Authorization**: Auth flow, session management, RBAC/permissions
- **Error Handling**: Error types, error boundaries, logging strategy
- **External Integrations**: Third-party APIs, SDKs, webhook handlers

### B. Inferred Documentation (from code patterns — high confidence)
- **Processing Flows**: Key business logic flows derived from function call chains
- **Security Design**: Security measures found in middleware, validation, sanitization
- **Performance Considerations**: Caching, lazy loading, optimization patterns found in code

## Rules for Multi-Repository Projects
- When multiple repositories form one project, create CROSS-REPOSITORY documentation
- Show how repositories interact with each other (e.g., frontend calls backend API)
- Create architecture pages that span all repositories
- Individual repository details should still get their own focused pages
- Clearly indicate which repository each page primarily covers

## General Rules
- Create pages ONLY for categories where substantial code evidence exists
- Each page MUST map to actual files in the repositories
- If a category has no corresponding code, do NOT create a page — simply omit it
- Prefer MORE pages with focused scope over FEWER pages with broad scope
- The number of pages should be proportional to the overall complexity
- A tiny CLI tool might need 3-5 pages; a large multi-repo project might need 30+
- Page titles should be specific
- If a domain has many sub-components, create separate pages for each
- Facts derived directly from code: always include
- High-confidence inferences from code patterns: include
- Pure speculation with no code evidence: NEVER include

## GitHub Wiki Link Format
Each page will become a separate .md file in a GitHub Wiki.
Page filenames: replace spaces with hyphens.
Link format: [Link Text](Page-Filename)

Return your analysis in the following XML format:

<wiki_structure>
  <title>[Overall wiki title]</title>
  <description>[Project description]</description>
  <pages>
    <page id="page-1">
      <title>[Page title]</title>
      <filename>[Page-Filename]</filename>
      <description>[What this page covers and WHY it's needed]</description>
      <importance>high|medium|low</importance>
      <relevant_files>
        <file_path>[Actual file path in the repo]</file_path>
      </relevant_files>
      <related_pages>
        <related>page-2</related>
      </related_pages>
    </page>
  </pages>
</wiki_structure>

IMPORTANT: Return ONLY valid XML. Start with <wiki_structure> and end with </wiki_structure>.
`, projectName, repoList, langName)
}

func pagePrompt(page WikiPage, allPages []WikiPage, projectName string, repos []string, language string) string {
	langName := languageName(language)
	repoList := strings.Join(repos, ", ")

	var pageList strings.Builder
	for _, p := range allPages {
		if p.ID != page.ID {
			pageList.WriteString(fmt.Sprintf("- [%s](%s) — %s\n", p.Title, p.Filename, p.Description))
		}
	}

	return fmt.Sprintf(`You are an expert technical writer creating a wiki page.

Project: %s
Repositories: %s

You have full access to ALL repository source code via the tools available to you.
USE the Read, Grep, Glob, and Bash tools to read actual source files before writing.
Do NOT guess or speculate — read the code first, then document what you find.

## Your Task
Write the wiki page: **%s**
Page description: %s

## Output Language
Write ALL content in %s.

## Other Wiki Pages (for cross-linking)
%s
When referencing other pages, use GitHub Wiki link format: [Page Title](Page-Filename)

## Content Requirements

### 1. Introduction (1-2 paragraphs)
- Purpose and scope of this page
- How this relates to the broader system
- Which repository/repositories this page covers
- Link to related wiki pages

### 2. Detailed Sections (use ## and ### headings)
- Explain architecture, components, data flow, or logic
- Identify key functions, classes, data structures, API endpoints
- For multi-repo projects: show cross-repository interactions

### 3. Mermaid Diagrams (EXTENSIVELY use these)
- Use flowchart TD (top-down, NEVER use LR), sequenceDiagram, classDiagram, erDiagram
- Every significant architectural concept should have a diagram
- For multi-repo: show inter-service communication
- Sequence diagrams: define ALL participants first, use proper arrow syntax
- Keep node labels to 3-4 words max

### 4. Tables
- Summarize structured information: API endpoints, config options, data fields

### 5. Code Snippets
- Include short, relevant code snippets directly from source files

### 6. Source Citations (CRITICAL)
- For EVERY significant claim, cite the source file and line numbers
- Format: Sources: [repo/filename.ext:lines]()
- You MUST cite AT LEAST 5 different source files

### 7. Cross-Page Links
- Link to related wiki pages using [Page Title](Page-Filename) format
- Add a "Related Pages" section at the end

## Quality Rules
- Facts from code: always include with source citations
- High-confidence inferences: include
- Pure speculation: NEVER include — omit entirely without mentioning the absence
- Be thorough — this is production-grade documentation

## Format Rules
- Start directly with the content (no preamble)
- First line should be a brief intro paragraph (# heading added automatically)
- Use ## for major sections, ### for subsections
- End with a "Related Pages" section
`, projectName, repoList, page.Title, page.Description, langName, pageList.String())
}

func languageName(code string) string {
	names := map[string]string{
		"ja": "Japanese (日本語)", "en": "English", "zh": "Mandarin Chinese (中文)",
		"zh-tw": "Traditional Chinese (繁體中文)", "es": "Spanish (Español)",
		"kr": "Korean (한국어)", "vi": "Vietnamese (Tiếng Việt)",
		"pt-br": "Brazilian Portuguese (Português Brasileiro)",
		"fr": "Français (French)", "ru": "Русский (Russian)",
	}
	if n, ok := names[code]; ok {
		return n
	}
	return "English"
}

// ── XML Parsing ──

func cleanXMLResponse(content string) string {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content, "\n"); idx != -1 {
			content = content[idx+1:]
		}
		if idx := strings.LastIndex(content, "```"); idx != -1 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)
	}
	if idx := strings.LastIndex(content, "</wiki_structure>"); idx != -1 {
		content = content[:idx+len("</wiki_structure>")]
	}
	content = strings.ReplaceAll(content, "/no_think", "")
	content = strings.ReplaceAll(content, "/think", "")
	return content
}

func parsePages(xml string) []WikiPage {
	var pages []WikiPage
	remaining := xml
	for {
		pageStart := strings.Index(remaining, "<page id=\"")
		if pageStart == -1 {
			break
		}
		remaining = remaining[pageStart:]

		idStart := strings.Index(remaining, "\"") + 1
		idEnd := strings.Index(remaining[idStart:], "\"") + idStart
		id := remaining[idStart:idEnd]

		title := extractTag(remaining, "title")
		filename := extractTag(remaining, "filename")
		desc := extractTag(remaining, "description")

		if filename == "" && title != "" {
			filename = titleToFilename(title)
		}

		if title != "" {
			pages = append(pages, WikiPage{
				ID: id, Title: title, Filename: filename, Description: desc,
			})
		}

		pageEnd := strings.Index(remaining, "</page>")
		if pageEnd == -1 {
			break
		}
		remaining = remaining[pageEnd+7:]
	}
	return pages
}

func extractTag(s string, tag string) string {
	open := fmt.Sprintf("<%s>", tag)
	close := fmt.Sprintf("</%s>", tag)
	start := strings.Index(s, open)
	if start == -1 {
		return ""
	}
	start += len(open)
	end := strings.Index(s[start:], close)
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(s[start : start+end])
}

func titleToFilename(title string) string {
	replacer := strings.NewReplacer(
		" ", "-", "/", "-", "\\", "-", ":", "-", "*", "", "?", "",
		"\"", "", "<", "", ">", "", "|", "", "（", "-", "）", "",
		"・", "-", "　", "-",
	)
	return replacer.Replace(title)
}

// ── Error Log ──

func appendError(dir, msg string) {
	errFile := filepath.Join(dir, "_errors.log")
	f, err := os.OpenFile(errFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), msg)
}

// ── Wiki Generation ──

func generateWiki(claudePath, model string, projectName string, repos []string, token, language, outputDir, cloneDir string, pageParallel int, useGH bool, progress *Progress) error {

	// Step 0: Clone all repositories
	var repoDirs []string
	for _, repo := range repos {
		repoURL := repo
		if !strings.HasPrefix(repo, "http") {
			repoURL = fmt.Sprintf("https://github.com/%s", repo)
		}

		parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(repoURL, "https://github.com/"), "https://gitlab.com/"), "/")
		if len(parts) < 2 {
			return fmt.Errorf("invalid repo format: %s", repo)
		}
		owner := parts[0]
		repoName := strings.TrimSuffix(parts[1], ".git")

		progress.set(projectName, fmt.Sprintf("📥 cloning %s/%s...", owner, repoName))
		repoDir := filepath.Join(cloneDir, fmt.Sprintf("%s_%s", owner, repoName))
		if err := gitClone(repoURL, token, repoDir, useGH); err != nil {
			return fmt.Errorf("clone %s: %w", repo, err)
		}
		repoDirs = append(repoDirs, repoDir)
	}

	// Step 1: Determine wiki structure
	progress.set(projectName, "📋 structure...")

	structureContent, err := claudeCall(claudePath, model, repoDirs, xmlSystemPrompt, structurePrompt(projectName, repos, language))
	if err != nil {
		return fmt.Errorf("structure: %w", err)
	}
	structureContent = cleanXMLResponse(structureContent)

	pages := parsePages(structureContent)
	if len(pages) == 0 {
		return fmt.Errorf("no pages found in structure")
	}
	log.Printf("[%s] Structure: %d pages", projectName, len(pages))

	// Create output directory
	wikiDir := filepath.Join(outputDir, projectName)
	if err := os.MkdirAll(wikiDir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	var allPages []WikiPage
	for _, p := range pages {
		allPages = append(allPages, p)
	}

	// Write Home.md and _Sidebar.md immediately
	writeHomeAndSidebar(wikiDir, projectName, structureContent, allPages, repos)

	// Step 2: Generate pages with parallelism
	var pageDone int32
	pageSem := make(chan struct{}, pageParallel)
	var pageWg sync.WaitGroup

	for i := range allPages {
		pageWg.Add(1)
		pageSem <- struct{}{}
		go func(idx int) {
			defer pageWg.Done()
			defer func() { <-pageSem }()

			page := &allPages[idx]
			progress.set(projectName, fmt.Sprintf("📝 %d/%d %s", atomic.LoadInt32(&pageDone)+1, len(allPages), page.Title))

			content, err := claudeCall(claudePath, model, repoDirs, "", pagePrompt(*page, allPages, projectName, repos, language))
			if err != nil {
				log.Printf("[%s] page %s failed: %v", projectName, page.Title, err)
				content = fmt.Sprintf("*Content generation failed: %v*", err)
				appendError(wikiDir, fmt.Sprintf("Page %d/%d: %s — %v", idx+1, len(allPages), page.Title, err))
			}

			page.Content = content

			filename := filepath.Join(wikiDir, page.Filename+".md")
			fileContent := fmt.Sprintf("# %s\n\n%s\n", page.Title, page.Content)
			if err := os.WriteFile(filename, []byte(fileContent), 0644); err != nil {
				appendError(wikiDir, fmt.Sprintf("Page %d/%d: %s — write failed: %v", idx+1, len(allPages), page.Title, err))
			}

			done := atomic.AddInt32(&pageDone, 1)
			log.Printf("[%s] Page %d/%d saved: %s (%d chars)", projectName, done, len(allPages), page.Title, len(content))
		}(i)
	}
	pageWg.Wait()

	log.Printf("[%s] ✅ completed %s/ (%d pages)", projectName, wikiDir, len(allPages))
	progress.done(projectName)
	return nil
}

func writeHomeAndSidebar(wikiDir, projectName, structureContent string, allPages []WikiPage, repos []string) {
	var home strings.Builder
	home.WriteString(fmt.Sprintf("# %s\n\n", projectName))
	desc := extractTag(structureContent, "description")
	if desc != "" {
		home.WriteString(fmt.Sprintf("%s\n\n", desc))
	}
	if len(repos) > 1 {
		home.WriteString("## Repositories\n\n")
		for _, r := range repos {
			home.WriteString(fmt.Sprintf("- %s\n", r))
		}
		home.WriteString("\n")
	}
	home.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	home.WriteString("## Pages\n\n")
	for _, page := range allPages {
		home.WriteString(fmt.Sprintf("- [%s](%s) — %s\n", page.Title, page.Filename, page.Description))
	}
	os.WriteFile(filepath.Join(wikiDir, "Home.md"), []byte(home.String()), 0644)

	var sidebar strings.Builder
	sidebar.WriteString("**[Home](Home)**\n\n---\n\n")
	for _, page := range allPages {
		sidebar.WriteString(fmt.Sprintf("- [%s](%s)\n", page.Title, page.Filename))
	}
	os.WriteFile(filepath.Join(wikiDir, "_Sidebar.md"), []byte(sidebar.String()), 0644)
}

// ── Repo List Parsing ──

// Parse repos.txt supporting both formats:
//   owner/repo              → standalone wiki
//   project:owner/repo      → grouped into one wiki
func parseRepoList(lines []string) (standalone []RepoEntry, groups map[string][]string) {
	groups = make(map[string][]string)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if before, after, ok := strings.Cut(line, ":"); ok && !strings.Contains(before, "/") {
			// project:owner/repo format
			groups[before] = append(groups[before], after)
		} else {
			// standalone owner/repo format
			standalone = append(standalone, RepoEntry{Repo: line})
		}
	}
	return
}

// ── Env Helpers ──

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		fmt.Sscan(v, &i)
		if i > 0 {
			return i
		}
	}
	return fallback
}

func envOrDefaultBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return fallback
}

func loadEnvFile() {
	for _, path := range []string{".env", ".env.local"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if k, v, ok := strings.Cut(line, "="); ok {
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)
				if os.Getenv(k) == "" {
					os.Setenv(k, v)
				}
			}
		}
	}
}

// ── Main ──

func main() {
	loadEnvFile()

	var (
		reposFile    string
		repos        string
		token        string
		model        string
		language     string
		outputDir    string
		cloneDir     string
		parallel     int
		pageParallel int
		logFile      string
		claudePath   string
		useGH        bool
	)

	flag.StringVar(&reposFile, "f", "", "file containing repo list (one per line)")
	flag.StringVar(&repos, "r", "", "comma-separated repo list (owner/repo or project:owner/repo)")
	flag.StringVar(&token, "token", os.Getenv("GITHUB_TOKEN"), "GitHub PAT (default: $GITHUB_TOKEN)")
	flag.StringVar(&model, "model", envOrDefault("CLAUDE_MODEL", ""), "claude model (e.g., haiku, sonnet, opus)")
	flag.StringVar(&language, "lang", envOrDefault("WIKI_LANGUAGE", "ja"), "output language")
	flag.StringVar(&outputDir, "o", envOrDefault("WIKI_OUTPUT_DIR", "./wiki-output"), "output directory")
	flag.StringVar(&cloneDir, "clone-dir", envOrDefault("WIKI_CLONE_DIR", "./.repos"), "clone directory")
	flag.StringVar(&claudePath, "claude", "claude", "path to claude binary")
	flag.IntVar(&parallel, "p", envOrDefaultInt("WIKI_PARALLEL", 1), "parallel projects/repos")
	flag.IntVar(&pageParallel, "pp", envOrDefaultInt("WIKI_PAGE_PARALLEL", 3), "parallel pages per project")
	flag.BoolVar(&useGH, "gh", envOrDefaultBool("WIKI_USE_GH", false), "use gh CLI for cloning")
	flag.StringVar(&logFile, "log", "", "log file path (default: stderr)")
	flag.Parse()

	if flag.NArg() > 0 && repos == "" {
		repos = strings.Join(flag.Args(), ",")
	}

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	if _, err := exec.LookPath(claudePath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: claude CLI not found. Install from https://claude.ai/claude-code\n")
		os.Exit(1)
	}

	// Collect all lines
	var lines []string
	if reposFile != "" {
		data, err := os.ReadFile(reposFile)
		if err != nil {
			log.Fatalf("Failed to read repos file: %v", err)
		}
		lines = append(lines, strings.Split(string(data), "\n")...)
	}
	if repos != "" {
		for _, r := range strings.Split(repos, ",") {
			lines = append(lines, r)
		}
	}

	standalone, groups := parseRepoList(lines)

	// Build task list
	type task struct {
		name  string
		repos []string
	}
	var tasks []task

	for _, entry := range standalone {
		parts := strings.Split(entry.Repo, "/")
		name := entry.Repo
		if len(parts) >= 2 {
			name = parts[len(parts)-1]
		}
		tasks = append(tasks, task{name: name, repos: []string{entry.Repo}})
	}
	for project, repoList := range groups {
		tasks = append(tasks, task{name: project, repos: repoList})
	}

	if len(tasks) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: wikigen [flags] owner/repo [owner/repo2 ...]")
		fmt.Fprintln(os.Stderr, "       wikigen -f repos.txt")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "repos.txt format:")
		fmt.Fprintln(os.Stderr, "  owner/repo                    # standalone wiki")
		fmt.Fprintln(os.Stderr, "  project:owner/repo1           # grouped into one wiki")
		fmt.Fprintln(os.Stderr, "  project:owner/repo2")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Prerequisites: git, claude CLI (authenticated)")
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "🚀 Processing %d wikis (parallel: %d, pages: %d, model: %s)\n\n",
		len(tasks), parallel, pageParallel,
		func() string {
			if model != "" {
				return model
			}
			return "default"
		}())

	progress := newProgress(len(tasks))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string
	start := time.Now()

	for _, t := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(t task) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := generateWiki(claudePath, model, t.name, t.repos, token, language, outputDir, cloneDir, pageParallel, useGH, progress); err != nil {
				log.Printf("[%s] ❌ %v", t.name, err)
				progress.done(t.name)
				mu.Lock()
				failed = append(failed, t.name)
				mu.Unlock()
			}
		}(t)
	}

	wg.Wait()

	elapsed := time.Since(start).Round(time.Second)
	fmt.Fprintf(os.Stderr, "\n\n✨ Done in %s — %d/%d succeeded\n", elapsed, len(tasks)-len(failed), len(tasks))
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "❌ Failed: %s\n", strings.Join(failed, ", "))
	}
	fmt.Fprintf(os.Stderr, "📁 Output: %s\n", outputDir)
}
