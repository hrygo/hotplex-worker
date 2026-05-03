export type Highlighter = typeof import('highlight.js/lib/core')['default'];

const LANGUAGES = [
  'bash', 'go', 'json', 'markdown', 'python', 'sql', 'typescript', 'yaml',
] as const;

let cached: Highlighter | null = null;
let pending: Promise<Highlighter> | null = null;

export function getHighlighter(): Promise<Highlighter> {
  if (cached) return Promise.resolve(cached);
  if (pending) return pending;

  pending = (async () => {
    const [coreModule, ...langModules] = await Promise.all([
      import('highlight.js/lib/core'),
      ...LANGUAGES.map(lang => import(`highlight.js/lib/languages/${lang}`)),
    ]);

    const hljs = coreModule.default;
    LANGUAGES.forEach((lang, i) => {
      hljs.registerLanguage(lang, langModules[i].default);
    });

    cached = hljs;
    pending = null;
    return hljs;
  })();

  return pending;
}
