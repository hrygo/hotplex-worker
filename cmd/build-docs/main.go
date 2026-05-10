package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
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

func buildNav(root string, discovered map[string]bool) []NavItem {
	return scanNav(root, "", discovered)
}

func scanNav(base, rel string, discovered map[string]bool) []NavItem {
	var items []NavItem
	dir := filepath.Join(base, rel)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			if entry.Name() == "assets" || entry.Name() == "archive" {
				continue
			}
			childRel := filepath.Join(rel, entry.Name())
			children := scanNav(base, childRel, discovered)
			if len(children) > 0 {
				items = append(items, NavItem{
					Title:    toTitle(entry.Name()),
					Path:     "",
					Children: children,
				})
			}
			continue
		}

		if filepath.Ext(entry.Name()) == ".md" {
			name := entry.Name()
			if name == "README.md" {
				continue
			}
			relPath := normalizePath(filepath.Join(rel, name))
			if !discovered[relPath] {
				continue
			}

			filePath := filepath.Join(dir, name)
			var meta struct {
				Title string `yaml:"title"`
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			_, _ = frontmatter.Parse(bytes.NewReader(data), &meta)

			title := meta.Title
			if title == "" {
				title = strings.TrimSuffix(name, ".md")
				title = strings.ReplaceAll(title, "-", " ")
				title = toTitle(title)
			}

			htmlPath := strings.TrimSuffix(relPath, ".md") + ".html"
			items = append(items, NavItem{
				Title: title,
				Path:  htmlPath,
			})
		}
	}
	return items
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

	var buf bytes.Buffer
	if err := md.Convert(rest, &buf); err != nil {
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
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&family=JetBrains+Mono:ital,wght@0,100..800;1,100..800&family=Noto+Serif+SC:wght@200..900&family=Outfit:wght@100..900&family=Source+Serif+4:ital,opsz,wght@0,8..60,200..900;1,8..60,200..900&display=swap" rel="stylesheet">
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
            background: var(--gray-100);
            border-right: 1px solid var(--gray-300);
            padding: 48px 32px;
            position: sticky;
            top: 0;
            height: 100vh;
            overflow-y: auto;
            display: flex;
            flex-direction: column;
        }

        .logo-area {
            margin-bottom: 48px;
        }

        .logo {
            font-family: var(--font-display);
            font-size: 26px;
            font-weight: 700;
            color: var(--slate);
            text-decoration: none;
            letter-spacing: -0.02em;
            display: flex;
            align-items: center;
            gap: 10px;
        }
        .logo::before {
            content: "";
            width: 12px;
            height: 12px;
            background: var(--clay);
            border-radius: 2px;
            transform: rotate(45deg);
        }

        nav .nav-section {
            margin-bottom: 32px;
        }

        nav .category {
            font-family: var(--font-mono);
            font-size: 10px;
            text-transform: uppercase;
            letter-spacing: 0.1em;
            color: var(--gray-500);
            margin-bottom: 12px;
            display: block;
        }

        nav ul { list-style: none; }
        nav li { margin-bottom: 4px; }
        nav a {
            text-decoration: none;
            color: var(--gray-500);
            font-size: 14px;
            padding: 6px 12px;
            border-radius: 6px;
            display: block;
            transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
            position: relative;
        }
        nav a:hover {
            background: var(--gray-200);
            color: var(--slate);
        }
        nav a.active {
            background: var(--white);
            color: var(--clay);
            font-weight: 600;
            box-shadow: 0 2px 8px rgba(0,0,0,0.05);
        }
        nav a.active::before {
            content: "";
            position: absolute;
            left: 0;
            top: 20%;
            bottom: 20%;
            width: 3px;
            background: var(--clay);
            border-radius: 0 2px 2px 0;
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
            <a href="{{.RelRoot}}index.html" class="logo">HOTPLEX</a>
        </div>
        <nav>
            {{range .Nav}}
                <div class="nav-section">
                {{if .Children}}
                    <div class="category">{{.Title}}</div>
                    <ul>
                        {{range .Children}}
                        <li><a href="{{$.RelRoot}}{{.Path}}" {{if eq $.ActivePath .Path}}class="active"{{end}}>{{.Title}}</a></li>
                        {{end}}
                    </ul>
                {{else}}
                    <li><a href="{{$.RelRoot}}{{.Path}}" {{if eq $.ActivePath .Path}}class="active"{{end}}>{{.Title}}</a></li>
                {{end}}
                </div>
            {{end}}
        </nav>
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
    <script type="module">
        import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs';
        mermaid.initialize({ 
            startOnLoad: true,
            theme: 'neutral',
            securityLevel: 'loose',
            fontFamily: 'Inter, system-ui, sans-serif'
        });

        // Convert mermaid code blocks
        document.querySelectorAll('pre code.language-mermaid').forEach(code => {
            const pre = code.parentElement;
            const container = document.createElement('pre');
            container.className = 'mermaid';
            container.textContent = code.textContent;
            pre.replaceWith(container);
        });

        // Add copy buttons to code blocks
        document.querySelectorAll('.prose pre').forEach(pre => {
            if (pre.classList.contains('mermaid')) return;
            const btn = document.createElement('button');
            btn.className = 'copy-btn';
            btn.textContent = 'COPY';
            btn.onclick = () => {
                const code = pre.querySelector('code').innerText;
                navigator.clipboard.writeText(code);
                btn.textContent = 'COPIED';
                setTimeout(() => btn.textContent = 'COPY', 2000);
            };
            pre.appendChild(btn);
        });
    </script>
</body>
</html>
`
