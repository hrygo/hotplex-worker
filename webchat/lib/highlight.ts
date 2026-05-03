type Highlighter = typeof import('highlight.js/lib/core')['default'];

let cached: Highlighter | null = null;
let pending: Promise<Highlighter> | null = null;

export function getHighlighter(): Promise<Highlighter> {
  if (cached) return Promise.resolve(cached);
  if (pending) return pending;

  pending = (async () => {
    const [coreModule, bash, go, json, markdown, python, sql, typescript, yaml] = await Promise.all([
      import('highlight.js/lib/core'),
      import('highlight.js/lib/languages/bash'),
      import('highlight.js/lib/languages/go'),
      import('highlight.js/lib/languages/json'),
      import('highlight.js/lib/languages/markdown'),
      import('highlight.js/lib/languages/python'),
      import('highlight.js/lib/languages/sql'),
      import('highlight.js/lib/languages/typescript'),
      import('highlight.js/lib/languages/yaml'),
    ]);

    const hljs = coreModule.default;

    hljs.registerLanguage('bash', bash.default);
    hljs.registerLanguage('go', go.default);
    hljs.registerLanguage('json', json.default);
    hljs.registerLanguage('markdown', markdown.default);
    hljs.registerLanguage('python', python.default);
    hljs.registerLanguage('sql', sql.default);
    hljs.registerLanguage('typescript', typescript.default);
    hljs.registerLanguage('yaml', yaml.default);

    cached = hljs;
    pending = null;
    return hljs;
  })();

  return pending;
}
