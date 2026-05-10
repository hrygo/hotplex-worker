package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/adrg/frontmatter"
	"github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	docsSrc  = "docs"
	docsDest = "internal/docs/out"
)

type Page struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Content     template.HTML
	Nav         []NavItem
	ActivePath  string
	RelRoot     string
}

type NavItem struct {
	Title    string
	Path     string // relative to root, e.g., "guides/user/chat-with-ai.html"
	Weight   int
	Active   bool
	Children []NavItem
}

var (
	md          goldmark.Markdown
	discoveryMd goldmark.Markdown
)

func init() {
	// For rendering (with link rewriting)
	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Table,
			extension.Typographer,
			highlighting.NewHighlighting(
				highlighting.WithStyle("monokai"),
				highlighting.WithFormatOptions(
					html.WithLineNumbers(false),
				),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(
				util.Prioritized(&linkTransformer{}, 100),
			),
		),
		goldmark.WithRendererOptions(
			gmhtml.WithUnsafe(),
		),
	)

	// For discovery (no link rewriting)
	discoveryMd = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
}

type linkTransformer struct{}

func (t *linkTransformer) Transform(node *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		if n.Kind() == ast.KindLink {
			if l, ok := n.(*ast.Link); ok {
				dest := string(l.Destination)
				if strings.HasSuffix(dest, ".md") {
					l.Destination = []byte(strings.TrimSuffix(dest, ".md") + ".html")
				} else if idx := strings.Index(dest, ".md#"); idx != -1 {
					l.Destination = []byte(dest[:idx] + ".html" + dest[idx+3:])
				}
			}
		}
		return ast.WalkContinue, nil
	})
}

func main() {
	// Clean destination
	if err := os.RemoveAll(docsDest); err != nil {
		log.Printf("Warning: failed to clean destination: %v", err)
	}
	if err := os.MkdirAll(docsDest, 0o755); err != nil {
		log.Fatalf("Error creating destination: %v", err)
	}

	// Phase 1: Discovery via crawling starting from index.md
	discoveredFiles := make(map[string]bool)
	discoveredAssets := make(map[string]bool)

	queue := []string{"index.md"}
	discoveredFiles["index.md"] = true

	for len(queue) > 0 {
		currentRel := queue[0]
		queue = queue[1:]

		path := filepath.Join(docsSrc, currentRel)
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Warning: could not read discovered file %s: %v", path, err)
			continue
		}

		// Extract links
		links, assets := extractLinks(data)
		for _, link := range links {
			// Resolve relative to currentRel
			dir := filepath.Dir(currentRel)
			resolved := normalizePath(filepath.Join(dir, link))
			if !discoveredFiles[resolved] {
				// Verify file exists
				if _, err := os.Stat(filepath.Join(docsSrc, resolved)); err == nil {
					discoveredFiles[resolved] = true
					queue = append(queue, resolved)
				}
			}
		}
		for _, asset := range assets {
			dir := filepath.Dir(currentRel)
			resolved := normalizePath(filepath.Join(dir, asset))
			discoveredAssets[resolved] = true
		}
	}

	fmt.Printf("Discovered %d documents and %d assets\n", len(discoveredFiles), len(discoveredAssets))

	// Build navigation tree (filtered by discovered files)
	nav := buildNav(docsSrc, discoveredFiles)

	// Phase 2: Copy discovered assets
	for asset := range discoveredAssets {
		src := filepath.Join(docsSrc, asset)
		dst := filepath.Join(docsDest, asset)
		if err := copyFile(src, dst); err != nil {
			log.Printf("Warning: failed to copy asset %s: %v", src, err)
		}
	}

	// Copy explicit assets
	if err := copyFile(filepath.Join("webchat", "public", "logo.png"), filepath.Join(docsDest, "assets", "logo.png")); err != nil {
		log.Printf("Warning: failed to copy logo.png: %v", err)
	}
	if err := copyFile(filepath.Join("webchat", "public", "logo.webp"), filepath.Join(docsDest, "assets", "logo.webp")); err != nil {
		log.Printf("Warning: failed to copy logo.webp: %v", err)
	}

	// Phase 3: Process discovered markdown files
	for relPath := range discoveredFiles {
		srcPath := filepath.Join(docsSrc, relPath)
		// Ensure directory exists in dest
		if err := os.MkdirAll(filepath.Join(docsDest, filepath.Dir(relPath)), 0o755); err != nil {
			log.Printf("Warning: failed to create directory for %s: %v", relPath, err)
		}

		if err := processFile(srcPath, nav); err != nil {
			log.Fatalf("Error processing %s: %v", srcPath, err)
		}
	}

	fmt.Println("Documentation built successfully!")
}

func extractLinks(data []byte) (links, assets []string) {
	// We need to parse without frontmatter for AST walk
	var meta map[string]interface{}
	rest, err := frontmatter.Parse(bytes.NewReader(data), &meta)
	if err != nil {
		rest = data
	}

	reader := text.NewReader(rest)
	doc := discoveryMd.Parser().Parse(reader)

	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n.Kind() {
		case ast.KindLink:
			if l, ok := n.(*ast.Link); ok {
				dest := string(l.Destination)
				if isLocalDoc(dest) {
					links = append(links, cleanLink(dest))
				} else if isLocalAsset(dest) {
					assets = append(assets, dest)
				}
			}
		case ast.KindImage:
			if img, ok := n.(*ast.Image); ok {
				dest := string(img.Destination)
				if isLocalAsset(dest) {
					assets = append(assets, dest)
				}
			}
		}
		return ast.WalkContinue, nil
	})
	return
}

func isLocalDoc(dest string) bool {
	return !strings.HasPrefix(dest, "http") && (strings.HasSuffix(dest, ".md") || strings.Contains(dest, ".md#"))
}

func isLocalAsset(dest string) bool {
	if strings.HasPrefix(dest, "http") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(dest))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif" || ext == ".svg" || ext == ".pdf"
}

func cleanLink(dest string) string {
	if idx := strings.Index(dest, "#"); idx != -1 {
		return dest[:idx]
	}
	return dest
}

func normalizePath(p string) string {
	return filepath.Clean(strings.ReplaceAll(p, "\\", "/"))
}

var categoryTranslations = map[string]string{
	"guides":       "用户指南",
	"tutorials":    "实践教程",
	"reference":    "参考文档",
	"explanation":  "原理解析",
	"architecture": "系统架构",
	"specs":        "技术规范",
	"security":     "安全中心",
	"superpowers":  "高级能力",
}

func translateCategory(name string) string {
	if t, ok := categoryTranslations[strings.ToLower(name)]; ok {
		return t
	}
	return toTitle(name)
}

var titleTranslations = map[string]string{
	"Remote Coding Agent":                      "远程开发助手",
	"Voice features":                           "语音交互功能",
	"Enterprise Deployment Guide":              "企业部署指南",
	"Security Hardening 企业安全加固指南":              "企业安全加固指南",
	"Integration Patterns Guide":               "系统集成模式",
	"Multi-Tenant Isolation Guide":             "多租户隔离指南",
	"Resource Limits Guide":                    "资源配额与限制",
	"Observability Guide":                      "可观测性指南",
	"Disaster Recovery Guide":                  "灾备与恢复指南",
	"Config Management Guide":                  "配置管理指南",
	"Compliance and Audit Guide":               "合规与审计指南",
	"Context window":                           "上下文窗口控制",
	"Cron automation":                          "定时自动化任务",
	"Mobile access":                            "移动端接入指南",
	"Multiple agents":                          "多智能体协同",
	"Security model":                           "安全模型配置",
	"Session management":                       "会话管理",
	"Tips and tricks":                          "进阶使用技巧",
	"Webchat setup":                            "WebChat 部署指南",
	"WebSocket Full-Duplex Communication Flow": "WebSocket 双工通信流",
	"Agent Event Protocol (AEP) v1":            "AEP v1 协议规范",
	"AEP v1 Appendix（时序 / 状态机 / Trace）":        "AEP v1 附录解析",
	"Admin api":                                "Admin API",
	"Aep protocol":                             "AEP 协议参考",
	"Security InputValidation":                 "输入验证与防注入",
	"Security Authentication":                  "安全认证架构",
	"AI Tool Policy":                           "AI 工具执行策略",
	"Env Whitelist Strategy":                   "环境白名单策略",
	"SSRF Protection":                          "SSRF 防御机制",
	"Claude Code Context Analysis":             "Claude Code 上下文分析",
	"Platform Messaging Architecture Diagrams": "消息平台架构图",
	"Worker Gateway Design":                    "Worker Gateway 设计",
	"OpenCode Server Context Analysis":         "OpenCode 上下文分析",
	"Agent Config Design":                      "Agent 配置设计",
	"Platform Messaging Extension":             "消息平台扩展机制",
	"Message Persistence":                      "消息持久化设计",
	"Agent config system":                      "Agent 配置系统",
	"Session lifecycle":                        "会话生命周期",
	"Why hotplex":                              "为什么选择 HotPlex",
	"Brain llm orchestration":                  "LLM 编排机制",
	"Cron design":                              "Cron 调度器设计",
}

func translateTitle(title string) string {
	if t, ok := titleTranslations[title]; ok {
		return t
	}
	for k, v := range titleTranslations {
		if strings.EqualFold(k, title) {
			return v
		}
	}
	return title
}

func buildNav(root string, discovered map[string]bool) []NavItem {
	var items []NavItem
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var files []os.DirEntry
	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			if e.Name() != "assets" && e.Name() != "archive" {
				dirs = append(dirs, e)
			}
		} else {
			files = append(files, e)
		}
	}

	// 1. Top-level files
	for _, e := range files {
		name := e.Name()
		if filepath.Ext(name) != ".md" || name == "README.md" {
			continue
		}
		if !discovered[name] {
			continue
		}
		title, weight := getFileMeta(filepath.Join(root, name), name)
		htmlPath := strings.TrimSuffix(name, ".md") + ".html"
		items = append(items, NavItem{
			Title:  title,
			Path:   htmlPath,
			Weight: weight,
		})
	}
	sortNavItems(items)

	// 2. Sections (Directories)
	for _, e := range dirs {
		name := e.Name()
		children := scanNavFiles(filepath.Join(root, name), name, discovered)
		if len(children) > 0 {
			sortNavItems(children)
			items = append(items, NavItem{
				Title:    translateCategory(name),
				Path:     "",
				Weight:   0, // Directories sort order could also be added later if needed
				Children: children,
			})
		}
	}

	// Sort top-level again in case we want to mix files and directories? No, files first, then dirs.
	return items
}

func scanNavFiles(dir, rel string, discovered map[string]bool) []NavItem {
	var items []NavItem
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			// Flatten subdirectories into the current section
			subItems := scanNavFiles(filepath.Join(dir, name), filepath.Join(rel, name), discovered)
			items = append(items, subItems...)
		} else if filepath.Ext(name) == ".md" {
			if name == "README.md" {
				continue
			}
			relPath := normalizePath(filepath.Join(rel, name))
			if !discovered[relPath] {
				continue
			}

			title, weight := getFileMeta(filepath.Join(dir, name), name)
			htmlPath := strings.TrimSuffix(relPath, ".md") + ".html"
			items = append(items, NavItem{
				Title:  title,
				Path:   htmlPath,
				Weight: weight,
			})
		}
	}
	return items
}

func getFileMeta(filePath, name string) (string, int) {
	var meta struct {
		Title  string `yaml:"title"`
		Weight int    `yaml:"weight"`
	}
	data, err := os.ReadFile(filePath)
	if err == nil {
		_, _ = frontmatter.Parse(bytes.NewReader(data), &meta)
	}

	title := meta.Title
	if title == "" {
		title = strings.TrimSuffix(name, ".md")
		title = strings.ReplaceAll(title, "-", " ")
		title = toTitle(title)
	}
	return translateTitle(title), meta.Weight
}

func sortNavItems(items []NavItem) {
	sort.SliceStable(items, func(i, j int) bool {
		wi := items[i].Weight
		if wi == 0 {
			wi = 9999
		}
		wj := items[j].Weight
		if wj == 0 {
			wj = 9999
		}
		if wi == wj {
			return items[i].Title < items[j].Title
		}
		return wi < wj
	})
}

func processFile(path string, nav []NavItem) error {
	relPath, _ := filepath.Rel(docsSrc, path)
	destPath := filepath.Join(docsDest, strings.TrimSuffix(relPath, ".md")+".html")

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var page Page
	rest, err := frontmatter.Parse(bytes.NewReader(data), &page)
	if err != nil {
		page.Title = strings.TrimSuffix(filepath.Base(path), ".md")
		rest = data
	}
	if page.Title == "" {
		page.Title = strings.TrimSuffix(filepath.Base(path), ".md")
		page.Title = strings.ReplaceAll(page.Title, "-", " ")
		page.Title = toTitle(page.Title)
	}
	page.Title = translateTitle(page.Title)

	// Parse the markdown
	reader := text.NewReader(rest)
	doc := md.Parser().Parse(reader)

	// Remove the first H1 heading if it exists (since we render it in the template)
	if doc.FirstChild() != nil && doc.FirstChild().Kind() == ast.KindHeading {
		h, ok := doc.FirstChild().(*ast.Heading)
		if ok && h.Level == 1 {
			doc.RemoveChild(doc, h)
		}
	}

	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, rest, doc); err != nil {
		return err
	}

	content := buf.String()
	// Removed naive strings.ReplaceAll, now handled by linkTransformer

	page.Content = template.HTML(content)
	page.Nav = nav
	page.ActivePath = strings.TrimSuffix(normalizePath(relPath), ".md") + ".html"

	levels := strings.Count(normalizePath(relPath), "/")
	page.RelRoot = strings.Repeat("../", levels)
	if page.RelRoot == "" {
		page.RelRoot = "./"
	}

	tmpl, err := template.New("layout").Parse(layout)
	if err != nil {
		return err
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(f, page); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func toTitle(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

const layout = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}} | HotPlex Docs</title>
    <link rel="icon" type="image/png" href="{{.RelRoot}}assets/logo.png">
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link rel="preload" href="https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&family=JetBrains+Mono:ital,wght@0,100..800;1,100..800&family=Noto+Serif+SC:wght@200..900&family=Outfit:wght@100..900&family=Source+Serif+4:ital,opsz,wght@0,8..60,200..900;1,8..60,200..900&display=swap" as="style" onload="this.onload=null;this.rel='stylesheet'">
    <noscript><link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&family=JetBrains+Mono:ital,wght@0,100..800;1,100..800&family=Noto+Serif+SC:wght@200..900&family=Outfit:wght@100..900&family=Source+Serif+4:ital,opsz,wght@0,8..60,200..900;1,8..60,200..900&display=swap"></noscript>
    <style>
        :root {
            --ivory:    #FAF9F5;
            --slate:    #0F0F0E;
            --clay:     #C35A3E;
            --oat:      #E3DACC;
            --olive:    #63744D;
            --gray-50:  #FBFBF9;
            --gray-100: #F5F4F0;
            --gray-200: #EBEAE4;
            --gray-300: #DCDAD1;
            --gray-500: #7A7973;
            --gray-700: #3D3D3A;
            --white:    #FFFFFF;
            
            --font-display: 'Outfit', system-ui, sans-serif;
            --font-serif: 'Source Serif 4', 'Noto Serif SC', ui-serif, Georgia, serif;
            --font-sans: 'Inter', 'Noto Sans SC', system-ui, sans-serif;
            --font-mono: 'JetBrains Mono', 'Fira Code', ui-monospace, monospace;
        }

        * { margin: 0; padding: 0; box-sizing: border-box; }

        html { scroll-behavior: smooth; }

        body {
            font-family: var(--font-sans);
            background: var(--ivory);
            color: var(--gray-700);
            line-height: 1.6;
            display: flex;
            min-height: 100vh;
            -webkit-font-smoothing: antialiased;
        }

        /* Sidebar */
        aside.sidebar {
            width: 300px;
            background: var(--gray-50);
            border-right: 1px solid var(--gray-200);
            padding: 40px 24px;
            position: sticky;
            top: 0;
            height: 100vh;
            overflow-y: auto;
            display: flex;
            flex-direction: column;
            scrollbar-width: thin;
            scrollbar-color: var(--gray-300) transparent;
        }
        aside.sidebar::-webkit-scrollbar {
            width: 4px;
        }
        aside.sidebar::-webkit-scrollbar-thumb {
            background: var(--gray-300);
            border-radius: 4px;
        }

        .logo-area {
            margin-bottom: 40px;
            padding: 0 12px;
            display: flex;
            align-items: center;
            justify-content: space-between;
        }

        .github-link {
            color: var(--gray-500);
            transition: color 0.2s;
            display: flex;
            align-items: center;
        }
        .github-link:hover {
            color: var(--slate);
        }
        .github-link svg {
            width: 20px;
            height: 20px;
            fill: currentColor;
        }

        .logo-container {
            display: flex;
            align-items: center;
            gap: 16px;
        }
        .logo-text {
            font-family: var(--font-display);
            font-size: 28px;
            font-weight: 800;
            color: var(--slate);
            text-decoration: none;
            letter-spacing: -0.02em;
            transition: opacity 0.2s;
        }
        .logo-text:hover {
            opacity: 0.8;
        }
        .logo-img-link {
            display: block;
        }
        .logo-img {
            width: 36px;
            height: 36px;
            border-radius: 8px;
            display: block;
            transition: all 0.3s cubic-bezier(0.25, 0.8, 0.25, 1);
            box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
        }
        .logo-img-link:hover .logo-img {
            transform: translateY(-2px);
            box-shadow: 0 6px 16px rgba(195, 90, 62, 0.25);
            filter: brightness(1.02);
        }

        nav .nav-section {
            margin-bottom: 12px;
        }

        nav .category {
            font-family: var(--font-display);
            font-weight: 700;
            font-size: 13px;
            color: var(--gray-700);
            margin-bottom: 4px;
            padding: 8px 12px;
            display: flex;
            align-items: center;
            justify-content: space-between;
            cursor: pointer;
            border-radius: 8px;
            transition: background 0.2s, color 0.2s;
            user-select: none;
        }
        nav .category:hover {
            background: var(--gray-200);
            color: var(--slate);
        }
        nav .category .chevron {
            width: 16px;
            height: 16px;
            fill: var(--gray-400);
            transition: transform 0.3s cubic-bezier(0.4, 0, 0.2, 1);
            transform: rotate(-90deg);
        }
        nav .nav-section.open .category .chevron {
            transform: rotate(0);
        }
        nav .nav-links {
            display: grid;
            grid-template-rows: 0fr;
            transition: grid-template-rows 0.3s cubic-bezier(0.4, 0, 0.2, 1);
        }
        nav .nav-section.open .nav-links {
            grid-template-rows: 1fr;
        }
        nav .nav-links ul {
            min-height: 0;
            overflow: hidden;
            list-style: none;
            padding: 0;
            margin: 0;
        }

        nav ul { list-style: none; padding: 0; margin: 0; }
        nav li { margin-bottom: 2px; }
        nav a {
            text-decoration: none;
            color: var(--gray-500);
            font-size: 14px;
            padding: 7px 12px;
            border-radius: 8px;
            display: block;
            transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
            position: relative;
            line-height: 1.5;
            font-weight: 500;
        }
        nav a:hover {
            background: rgba(0,0,0,0.03);
            color: var(--slate);
        }
        nav a.active {
            background: var(--white);
            color: var(--clay);
            font-weight: 600;
            box-shadow: 0 1px 3px rgba(0,0,0,0.04), 0 1px 2px rgba(0,0,0,0.02);
            border: 1px solid rgba(0,0,0,0.04);
        }

        /* Content */
        main.content {
            flex: 1;
            padding: 80px 80px 120px;
            background: var(--white);
        }

        article {
            max-width: 840px;
            margin: 0 auto;
        }

        header.article-head { margin-bottom: 64px; }
        .eyebrow {
            font-family: var(--font-mono);
            font-size: 12px;
            color: var(--clay);
            margin-bottom: 16px;
            text-transform: uppercase;
            font-weight: 600;
            letter-spacing: 0.05em;
        }
        h1 {
            font-family: var(--font-serif);
            font-size: 48px;
            font-weight: 700;
            line-height: 1.15;
            color: var(--slate);
            margin-bottom: 20px;
            letter-spacing: -0.01em;
        }
        .description { 
            font-size: 20px; 
            color: var(--gray-500); 
            line-height: 1.5;
            font-family: var(--font-sans);
        }

        /* Markdown Styles */
        .prose h2 {
            font-family: var(--font-serif);
            font-size: 32px;
            font-weight: 600;
            color: var(--slate);
            margin: 64px 0 24px;
            padding-top: 12px;
            border-top: 1px solid var(--gray-200);
        }
        .prose h3 {
            font-family: var(--font-serif);
            font-size: 24px;
            font-weight: 600;
            color: var(--slate);
            margin: 40px 0 16px;
        }
        .prose hr {
            border: none;
            height: 1px;
            background: var(--gray-200);
            margin: 48px 0;
        }
        .prose hr + h2 {
            border-top: none;
            margin-top: 0;
            padding-top: 0;
        }
        .prose p { 
            margin-bottom: 24px; 
            font-size: 17px; 
            line-height: 1.7;
        }
        .prose ul, .prose ol { margin-bottom: 24px; padding-left: 28px; }
        .prose li { margin-bottom: 10px; font-size: 17px; }
        
        .prose a {
            color: var(--clay);
            text-decoration: none;
            border-bottom: 1px solid rgba(195, 90, 62, 0.2);
            transition: border-color 0.2s;
        }
        .prose a:hover { border-bottom-color: var(--clay); }

        /* Tables */
        .prose table {
            width: 100%;
            border-collapse: separate;
            border-spacing: 0;
            margin: 40px 0;
            font-size: 15px;
            border: 1px solid var(--gray-300);
            border-radius: 12px;
            overflow: hidden;
        }
        .prose th {
            background: var(--gray-50);
            text-align: left;
            padding: 14px 20px;
            border-bottom: 1px solid var(--gray-300);
            color: var(--slate);
            font-weight: 600;
            font-family: var(--font-display);
        }
        .prose td {
            padding: 14px 20px;
            border-bottom: 1px solid var(--gray-100);
            color: var(--gray-700);
        }
        .prose tr:last-child td { border-bottom: none; }

        /* Code Blocks */
        .prose pre {
            background: var(--slate) !important;
            color: var(--gray-100);
            border-radius: 16px;
            padding: 28px;
            margin: 40px 0;
            overflow-x: auto;
            border: 1px solid rgba(255,255,255,0.05);
            box-shadow: 0 20px 50px rgba(0,0,0,0.15);
            position: relative;
        }
        .prose code {
            font-family: var(--font-mono);
            font-size: 14px;
            line-height: 1.6;
        }
        .prose :not(pre) > code {
            background: var(--gray-100);
            padding: 3px 8px;
            border-radius: 6px;
            font-size: 0.85em;
            color: var(--clay);
            font-weight: 500;
            border: 1px solid var(--gray-300);
        }

        /* Mermaid */
        .mermaid {
            background: var(--white);
            padding: 32px;
            border-radius: 16px;
            margin: 48px 0;
            border: 1px solid var(--gray-300);
            display: flex;
            justify-content: center;
            box-shadow: 0 4px 20px rgba(0,0,0,0.03);
            transition: transform 0.3s ease;
        }
        .mermaid:hover {
            transform: translateY(-2px);
            box-shadow: 0 8px 30px rgba(0,0,0,0.06);
        }
        .mermaid:not([data-processed="true"]) {
            visibility: hidden;
            height: 0;
            margin: 0;
            padding: 0;
        }

        /* Callouts / Blockquotes */
        .prose blockquote {
            border-left: 4px solid var(--clay);
            background: var(--gray-50);
            padding: 32px 40px;
            margin: 48px 0;
            border-radius: 0 16px 16px 0;
            font-family: var(--font-serif);
            font-style: italic;
            font-size: 1.1em;
            color: var(--slate);
        }

        .prose img {
            max-width: 100%;
            border-radius: 16px;
            margin: 48px 0;
            box-shadow: 0 10px 40px rgba(0,0,0,0.08);
        }

        footer {
            margin-top: 120px;
            padding-top: 48px;
            border-top: 1px solid var(--gray-200);
            color: var(--gray-500);
            font-size: 13px;
            text-align: center;
            font-family: var(--font-mono);
            letter-spacing: 0.02em;
        }

        /* Copy Button (JS Generated) */
        .copy-btn {
            position: absolute;
            top: 12px;
            right: 12px;
            background: rgba(255,255,255,0.1);
            border: 1px solid rgba(255,255,255,0.2);
            color: white;
            padding: 4px 10px;
            border-radius: 6px;
            font-size: 11px;
            font-family: var(--font-mono);
            cursor: pointer;
            opacity: 0;
            transition: all 0.2s;
        }
        .prose pre:hover .copy-btn { opacity: 1; }
        .copy-btn:hover { background: rgba(255,255,255,0.2); }

        @media (max-width: 1024px) {
            aside.sidebar { width: 260px; padding: 40px 24px; }
            main.content { padding: 60px 40px; }
        }

        @media (max-width: 768px) {
            body { flex-direction: column; }
            aside.sidebar { width: 100%; height: auto; position: relative; border-right: none; border-bottom: 1px solid var(--gray-300); }
            main.content { padding: 40px 24px; }
            h1 { font-size: 36px; }
        }
    </style>
</head>
<body>
    <aside class="sidebar">
        <div class="logo-area">
            <div class="logo-container">
                <a href="/" class="logo-img-link" title="Back to WebChat">
                    <img src="{{.RelRoot}}assets/logo.webp" alt="HotPlex" class="logo-img">
                </a>
                <a href="{{.RelRoot}}index.html" class="logo-text" title="Docs Home">
                    HOTPLEX
                </a>
            </div>
            <a href="https://github.com/hrygo/hotplex" target="_blank" rel="noopener noreferrer" class="github-link" title="GitHub Repository">
                <svg viewBox="0 0 24 24"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/></svg>
            </a>
        </div>
        <nav>
            {{range .Nav}}
                <div class="nav-section">
                {{if .Children}}
                    <div class="category" onclick="this.parentElement.classList.toggle('open')">
                        {{.Title}}
                        <svg class="chevron" viewBox="0 0 24 24"><path d="M7 10l5 5 5-5z"/></svg>
                    </div>
                    <div class="nav-links">
                        <ul>
                            {{range .Children}}
                            <li><a href="{{$.RelRoot}}{{.Path}}" {{if eq $.ActivePath .Path}}class="active"{{end}}>{{.Title}}</a></li>
                            {{end}}
                        </ul>
                    </div>
                {{else}}
                    <li><a href="{{$.RelRoot}}{{.Path}}" {{if eq $.ActivePath .Path}}class="active"{{end}}>{{.Title}}</a></li>
                {{end}}
                </div>
            {{end}}
        </nav>
        <script>
            // Automatically open sections that contain the active page, or match the default preference
            document.querySelectorAll('.nav-section').forEach(section => {
                const titleNode = section.querySelector('.category');
                const hasActive = section.querySelector('.active');
                if (hasActive || (titleNode && titleNode.textContent.includes('原理解析'))) {
                    section.classList.add('open');
                }
            });
        </script>
    </aside>
    <main class="content">
        <article>
            <header class="article-head">
                <div class="eyebrow">Documentation</div>
                {{if .Title}}<h1 id="top">{{.Title}}</h1>{{end}}
                {{if .Description}}<p class="description">{{.Description}}</p>{{end}}
            </header>
            <div class="prose">
                {{.Content}}
            </div>
            <footer>
                HOTPLEX &bull; BUILT FOR THE FUTURE OF AGENTIC CODING &bull; 2026
            </footer>
        </article>
    </main>
    <script defer src="https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js"></script>
    <script>
        window.addEventListener('load', function() {
            if (typeof mermaid !== 'undefined') {
                // 1. Convert code blocks to div.mermaid
                const blocks = document.querySelectorAll('pre code.language-mermaid, pre code.language-graph');
                blocks.forEach(code => {
                    const pre = code.parentElement;
                    const div = document.createElement('div');
                    div.className = 'mermaid';
                    div.style.background = 'white'; // Ensure visible background
                    div.textContent = code.textContent;
                    pre.replaceWith(div);
                });

                // 2. Initialize and Render
                mermaid.initialize({ 
                    startOnLoad: false,
                    theme: 'neutral',
                    securityLevel: 'loose'
                });
                mermaid.init(undefined, '.mermaid');
            }

            // Add copy buttons to code blocks
            document.querySelectorAll('.prose pre').forEach(pre => {
                if (pre.classList.contains('mermaid')) return;
                const btn = document.createElement('button');
                btn.className = 'copy-btn';
                btn.textContent = 'COPY';
                btn.onclick = () => {
                    const codeBlock = pre.querySelector('code');
                    if (codeBlock) {
                        navigator.clipboard.writeText(codeBlock.innerText);
                        btn.textContent = 'COPIED';
                        setTimeout(() => btn.textContent = 'COPY', 2000);
                    }
                };
                pre.appendChild(btn);
            });
        });
    </script>
</body>
</html>
`
