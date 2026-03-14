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
	totalRepos int
	doneRepos  int32
	current    map[string]string
}

func newProgress(total int) *Progress {
	return &Progress{totalRepos: total, current: make(map[string]string)}
}

func (p *Progress) set(repo, status string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current[repo] = status
	p.print()
}

func (p *Progress) done(repo string) {
	atomic.AddInt32(&p.doneRepos, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.current, repo)
	p.print()
}

func (p *Progress) print() {
	done := int(atomic.LoadInt32(&p.doneRepos))
	fmt.Fprintf(os.Stderr, "\r\033[K[%d/%d repos] ", done, p.totalRepos)
	var parts []string
	for repo, status := range p.current {
		parts = append(parts, fmt.Sprintf("%s: %s", repo, status))
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

// ── Claude CLI ──

func claudeCall(claudePath, model, repoDir, systemPrompt, prompt string) (string, error) {
	args := []string{"-p", "--output-format", "text", "--dangerously-skip-permissions"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if repoDir != "" {
		args = append(args, "--add-dir", repoDir)
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

func gitClone(repoURL, token, destDir string, useGH bool) error {
	if _, err := os.Stat(filepath.Join(destDir, ".git")); err == nil {
		// Already cloned, pull latest
		cmd := exec.Command("git", "-C", destDir, "pull", "--ff-only")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	if useGH {
		// Use gh CLI (uses gh's own auth, no PAT needed)
		cmd := exec.Command("gh", "repo", "clone", repoURL, destDir, "--", "--depth=1", "--single-branch")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
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

func structurePrompt(short, language string) string {
	langName := languageName(language)
	return fmt.Sprintf(`You are an expert software architect and technical writer.
Analyze the GitHub repository %s and design a comprehensive wiki structure for it.

You have full access to the repository source code via the tools available to you.
USE the Read, Grep, Glob, and Bash tools to thoroughly examine the codebase before designing the structure.

Your task is to determine what documentation pages are needed based on WHAT ACTUALLY EXISTS in the codebase.
Do NOT create pages for things that don't exist in the repository.

IMPORTANT: All content will be written in %s.

## Documentation Categories

Analyze the repository and create pages from the following categories AS APPLICABLE:

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

## Rules
- Create pages ONLY for categories where substantial code evidence exists
- Each page MUST map to actual files in the repository
- If a category has no corresponding code, do NOT create a page for it — simply omit it
- Prefer MORE pages with focused scope over FEWER pages with broad scope
- The number of pages should be proportional to the repository's complexity and size
- A tiny CLI tool might need 3-5 pages; a large monorepo might need 30+
- Page titles should be specific (e.g., "REST API: User Endpoints" not just "API")
- If a domain has many sub-components (e.g., 10 API endpoint groups), create separate pages for each
- Facts derived directly from code: always include
- High-confidence inferences from code patterns: include
- Pure speculation with no code evidence: NEVER include

## GitHub Wiki Link Format
Each page will become a separate .md file in a GitHub Wiki.
Page filenames will be derived from titles by replacing spaces with hyphens.
When referencing other pages, use: [Link Text](Page-Filename)

Return your analysis in the following XML format:

<wiki_structure>
  <title>[Overall wiki title]</title>
  <description>[Repository description]</description>
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
`, short, langName)
}

func pagePrompt(page WikiPage, allPages []WikiPage, short, language, repoURL string) string {
	langName := languageName(language)

	var pageList strings.Builder
	for _, p := range allPages {
		if p.ID != page.ID {
			pageList.WriteString(fmt.Sprintf("- [%s](%s) — %s\n", p.Title, p.Filename, p.Description))
		}
	}

	return fmt.Sprintf(`You are an expert technical writer creating a wiki page for the %s repository.
Repository URL: %s

You have full access to the repository source code via the tools available to you.
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
- Purpose and scope of this page within the overall project
- How this component/feature relates to the broader system
- Link to related wiki pages where relevant

### 2. Detailed Sections (use ## and ### headings)
For each section:
- Explain the architecture, components, data flow, or logic
- Identify key functions, classes, data structures, API endpoints
- Show how components interact with each other

### 3. Mermaid Diagrams (EXTENSIVELY use these)
- Use flowchart TD (top-down, NEVER use LR), sequenceDiagram, classDiagram, erDiagram
- Every significant architectural concept should have a diagram
- Sequence diagrams: define ALL participants first, use proper arrow syntax
- Keep node labels to 3-4 words max

### 4. Tables
- Summarize structured information: API endpoints, config options, data fields, component props

### 5. Code Snippets
- Include short, relevant code snippets directly from source files
- Use proper language identifiers in code blocks

### 6. Source Citations (CRITICAL)
- For EVERY significant claim, cite the source file and line numbers
- Format: Sources: [filename.ext:start_line-end_line](%s/blob/main/filename.ext#Lstart-Lend)
- You MUST cite AT LEAST 5 different source files throughout the page

### 7. Cross-Page Links
- Link to related wiki pages using [Page Title](Page-Filename) format
- Add a "Related Pages" section at the end

## Quality Rules
- Facts from code: always include with source citations
- High-confidence inferences from code patterns: include
- Pure speculation with no code evidence: NEVER include — omit entirely without mentioning the absence
- Do NOT write "this could not be determined" — just leave it out
- Be thorough — this is production-grade documentation

## Format Rules
- Start directly with the content (no preamble)
- First line should be a brief intro paragraph (the # heading will be added automatically)
- Use ## for major sections, ### for subsections
- End with a "Related Pages" section
`, short, repoURL, page.Title, page.Description, langName, pageList.String(), repoURL)
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
	// Remove /no_think tags
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
				ID:          id,
				Title:       title,
				Filename:    filename,
				Description: desc,
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

func appendError(repoDir, msg string) {
	errFile := filepath.Join(repoDir, "_errors.log")
	f, err := os.OpenFile(errFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("15:04:05"), msg)
}

// ── Wiki Generation ──

func generateWiki(claudePath, model, repo, token, language, outputDir, cloneDir string, pageParallel int, useGH bool, progress *Progress) error {
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
	short := fmt.Sprintf("%s/%s", owner, repoName)

	// Step 0: Clone repository
	progress.set(short, "📥 cloning...")
	repoDir := filepath.Join(cloneDir, fmt.Sprintf("%s_%s", owner, repoName))
	if err := gitClone(repoURL, token, repoDir, useGH); err != nil {
		return fmt.Errorf("clone: %w", err)
	}

	// Step 1: Determine wiki structure
	progress.set(short, "📋 structure...")

	structureContent, err := claudeCall(claudePath, model, repoDir, xmlSystemPrompt, structurePrompt(short, language))
	if err != nil {
		return fmt.Errorf("structure: %w", err)
	}
	structureContent = cleanXMLResponse(structureContent)

	pages := parsePages(structureContent)
	if len(pages) == 0 {
		return fmt.Errorf("no pages found in structure")
	}
	log.Printf("[%s] Structure: %d pages", short, len(pages))

	// Create output directory
	wikiDir := filepath.Join(outputDir, repoName)
	if err := os.MkdirAll(wikiDir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Collect all pages for cross-linking
	var allPages []WikiPage
	for _, p := range pages {
		allPages = append(allPages, p)
	}

	// Write Home.md and _Sidebar.md immediately
	writeHomeAndSidebar(wikiDir, short, structureContent, allPages)

	// Step 2: Generate pages (with page-level parallelism)
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
			progress.set(short, fmt.Sprintf("📝 %d/%d %s", atomic.LoadInt32(&pageDone)+1, len(allPages), page.Title))

			content, err := claudeCall(claudePath, model, repoDir, "", pagePrompt(*page, allPages, short, language, repoURL))
			if err != nil {
				log.Printf("[%s] page %s failed: %v", short, page.Title, err)
				content = fmt.Sprintf("*Content generation failed: %v*", err)
				appendError(wikiDir, fmt.Sprintf("Page %d/%d: %s — %v", idx+1, len(allPages), page.Title, err))
			}

			page.Content = content

			// Save immediately
			filename := filepath.Join(wikiDir, page.Filename+".md")
			fileContent := fmt.Sprintf("# %s\n\n%s\n", page.Title, page.Content)
			if err := os.WriteFile(filename, []byte(fileContent), 0644); err != nil {
				appendError(wikiDir, fmt.Sprintf("Page %d/%d: %s — write failed: %v", idx+1, len(allPages), page.Title, err))
			}

			done := atomic.AddInt32(&pageDone, 1)
			log.Printf("[%s] Page %d/%d saved: %s (%d chars)", short, done, len(allPages), page.Title, len(content))
		}(i)
	}
	pageWg.Wait()

	log.Printf("[%s] ✅ completed %s/ (%d pages)", short, wikiDir, len(allPages))
	progress.done(short)
	return nil
}

func writeHomeAndSidebar(wikiDir, short, structureContent string, allPages []WikiPage) {
	var home strings.Builder
	home.WriteString(fmt.Sprintf("# %s\n\n", short))
	desc := extractTag(structureContent, "description")
	if desc != "" {
		home.WriteString(fmt.Sprintf("%s\n\n", desc))
	}
	home.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	home.WriteString("## Pages\n\n")
	for _, page := range allPages {
		home.WriteString(fmt.Sprintf("- [%s](%s) — %s\n", page.Title, page.Filename, page.Description))
	}
	os.WriteFile(filepath.Join(wikiDir, "Home.md"), []byte(home.String()), 0644)

	var sidebar strings.Builder
	sidebar.WriteString("**[Home](Home)**\n\n")
	sidebar.WriteString("---\n\n")
	for _, page := range allPages {
		sidebar.WriteString(fmt.Sprintf("- [%s](%s)\n", page.Title, page.Filename))
	}
	os.WriteFile(filepath.Join(wikiDir, "_Sidebar.md"), []byte(sidebar.String()), 0644)
}

// ── Env & Main ──

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := fmt.Sscanf(v, "%d"); err == nil && n > 0 {
			var i int
			fmt.Sscan(v, &i)
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

func main() {
	loadEnvFile()

	var (
		reposFile string
		repos     string
		token     string
		model     string
		language  string
		outputDir string
		cloneDir  string
		parallel     int
		pageParallel int
		logFile      string
		claudePath   string
		useGH        bool
	)

	flag.StringVar(&reposFile, "f", "", "file containing repo list (one per line)")
	flag.StringVar(&repos, "r", "", "comma-separated repo list (owner/repo)")
	flag.StringVar(&token, "token", os.Getenv("GITHUB_TOKEN"), "GitHub PAT (default: $GITHUB_TOKEN)")
	flag.StringVar(&model, "model", envOrDefault("CLAUDE_MODEL", ""), "claude model (e.g., haiku, sonnet, opus)")
	flag.StringVar(&language, "lang", envOrDefault("WIKI_LANGUAGE", "ja"), "output language")
	flag.StringVar(&outputDir, "o", envOrDefault("WIKI_OUTPUT_DIR", "./wiki-output"), "output directory for wiki files")
	flag.StringVar(&cloneDir, "clone-dir", envOrDefault("WIKI_CLONE_DIR", "./.repos"), "directory for cloned repositories")
	flag.StringVar(&claudePath, "claude", "claude", "path to claude binary")
	flag.IntVar(&parallel, "p", envOrDefaultInt("WIKI_PARALLEL", 1), "number of repos to process in parallel")
	flag.IntVar(&pageParallel, "pp", envOrDefaultInt("WIKI_PAGE_PARALLEL", 3), "number of pages to generate in parallel per repo")
	flag.BoolVar(&useGH, "gh", envOrDefaultBool("WIKI_USE_GH", false), "use gh CLI for cloning (no PAT needed)")
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

	// Verify claude is available
	if _, err := exec.LookPath(claudePath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: claude CLI not found. Install from https://claude.ai/claude-code\n")
		os.Exit(1)
	}

	var repoList []string
	if reposFile != "" {
		data, err := os.ReadFile(reposFile)
		if err != nil {
			log.Fatalf("Failed to read repos file: %v", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				repoList = append(repoList, line)
			}
		}
	}
	if repos != "" {
		for _, r := range strings.Split(repos, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				repoList = append(repoList, r)
			}
		}
	}

	if len(repoList) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: wikigen [flags] owner/repo [owner/repo2 ...]")
		fmt.Fprintln(os.Stderr, "       wikigen -r owner/repo1,owner/repo2")
		fmt.Fprintln(os.Stderr, "       wikigen -f repos.txt")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Prerequisites: git, claude CLI (authenticated)")
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "🚀 Processing %d repos (parallel: %d, model: %s, output: %s)\n\n",
		len(repoList), parallel, func() string { if model != "" { return model }; return "default" }(), outputDir)

	progress := newProgress(len(repoList))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []string
	start := time.Now()

	for _, repo := range repoList {
		wg.Add(1)
		sem <- struct{}{}
		go func(r string) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := generateWiki(claudePath, model, r, token, language, outputDir, cloneDir, pageParallel, useGH, progress); err != nil {
				log.Printf("[%s] ❌ %v", r, err)
				progress.done(r)
				mu.Lock()
				failed = append(failed, r)
				mu.Unlock()
			}
		}(repo)
	}

	wg.Wait()

	elapsed := time.Since(start).Round(time.Second)
	fmt.Fprintf(os.Stderr, "\n\n✨ Done in %s — %d/%d succeeded\n", elapsed, len(repoList)-len(failed), len(repoList))
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "❌ Failed: %s\n", strings.Join(failed, ", "))
	}
	fmt.Fprintf(os.Stderr, "📁 Output: %s\n", outputDir)
}
