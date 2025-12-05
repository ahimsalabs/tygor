import satori from 'satori';
import fs from 'fs/promises';

// Fetch fonts - using noto sans for better compatibility
const fontData = await fetch('https://cdn.jsdelivr.net/npm/@fontsource/noto-sans@5.0.0/files/noto-sans-latin-400-normal.woff').then(res => res.arrayBuffer());
const fontBoldData = await fetch('https://cdn.jsdelivr.net/npm/@fontsource/noto-sans@5.0.0/files/noto-sans-latin-700-normal.woff').then(res => res.arrayBuffer());
const fontMonoData = await fetch('https://cdn.jsdelivr.net/npm/@fontsource/roboto-mono@5.0.0/files/roboto-mono-latin-400-normal.woff').then(res => res.arrayBuffer());
const fontRubikGlitchData = await fetch('https://fonts.gstatic.com/s/rubikglitch/v2/qkBSXv8b_srFRYQVYrDKh9ZvmA.ttf').then(res => res.arrayBuffer());

// Read mascot SVG
const mascotSvg = await fs.readFile('./tygor.svg', 'utf-8');
const mascotDataUrl = `data:image/svg+xml;base64,${Buffer.from(mascotSvg).toString('base64')}`;

// Syntax highlighting colors
const colors = {
  keyword: 'oklch(0.50 0.12 45)', // brown/darker orange
  string: 'oklch(0.55 0.12 160)', // teal/cyan
  comment: '#888',
  type: 'oklch(0.45 0.10 50)', // darker orange-brown
  default: '#222',
};

// Helper to create syntax-highlighted code
const span = (text, color = colors.default) => ({
  type: 'span',
  props: {
    style: { color },
    children: text,
  },
});

// Helper to create a line of code
const line = (...children) => ({
  type: 'div',
  props: {
    style: { display: 'flex', whiteSpace: 'pre' },
    children,
  },
});

// Go code highlighting
const goCode = [
  line(span(' ')),
  line(span('func', colors.keyword), span(' ListNews(ctx, req *ListNewsParams) ([]*News, '), span('error', colors.type), span(') { ... }')),
  line(span('func', colors.keyword), span(' CreateNews(ctx, req *CreateNewsParams) (*News, '), span('error', colors.type), span(') { ... }')),
  line(span(' ')),
  line(span('app := tygor.NewApp()')),
  line(span('news := app.Service('), span('"News"', colors.string), span(')')),
  line(span('news.Register('), span('"List"', colors.string), span(', tygor.Query(ListNews))')),
  line(span('news.Register('), span('"Create"', colors.string), span(', tygor.Exec(CreateNews))')),
  line(span(' ')),
  line(span('if', colors.keyword), span(' *gen {')),
  line(span('  tygorgen.FromApp(app).ToDir('), span('"./client/api"', colors.string), span(')')),
  line(span('} ', colors.keyword), span('else', colors.keyword), span(' {')),
  line(span('  http.ListenAndServe('), span('":8080"', colors.string), span(', app.Handler())')),
  line(span('}')),
];

// TypeScript code highlighting
const tsCode = [
  line(span('import', colors.keyword), span(' { createClient } '), span('from', colors.keyword), span(' '), span("'@tygor/client'", colors.string)),
  line(span('import', colors.keyword), span(' { registry } '), span('from', colors.keyword), span(' '), span("'./api/manifest'", colors.string)),
  line(span(' ')),
  line(span('const', colors.keyword), span(' client = createClient(registry, config)')),
  line(span(' ')),
  line(span('// Fully typed', colors.comment)),
  line(span('const', colors.keyword), span(' news = '), span('await', colors.keyword), span(' client.News.List({ limit: 10 })')),
  line(span('news[0].title '), span('// string', colors.comment)),
  line(span(' ')),
  line(span('const', colors.keyword), span(' created = '), span('await', colors.keyword), span(' client.News.Create({')),
  line(span('  title: '), span('"Breaking"', colors.string), span(',')),
  line(span('  body: '), span('"News!"', colors.string)),
  line(span('})')),
];

const markup = {
  type: 'div',
  props: {
    style: {
      display: 'flex',
      flexDirection: 'column',
      width: '100%',
      height: '100%',
      background: 'linear-gradient(135deg, #FFF8F0 0%, #FFF5E8 100%)',
      padding: 20,
      color: '#FFAC42',
      fontFamily: 'Noto Sans',
    },
    children: [
      // Header
      {
        type: 'div',
        props: {
          style: { display: 'flex', alignItems: 'center', gap: 12, marginBottom: 32 },
          children: [
            // Mascot
            {
              type: 'img',
              props: {
                src: mascotDataUrl,
                style: { width: 200, height: 200 },
              },
            },
            // Title and tagline
            {
              type: 'div',
              props: {
                style: { display: 'flex', flexDirection: 'column' },
                children: [
                  {
                    type: 'div',
                    props: {
                      style: { fontSize: 76, fontWeight: 400, letterSpacing: '-0.02em', marginBottom: 8, fontFamily: 'Rubik Glitch', color: 'oklch(0.55 0.15 50)' },
                      children: 'tygor',
                    },
                  },
                  {
                    type: 'div',
                    props: {
                      style: { fontSize: 20, maxWidth: 900, lineHeight: 1.4, color: 'oklch(0.55 0.15 50)' },
                      children: 'Type-safe backend for Go + TypeScript apps',
                    },
                  },
                ],
              },
            },
          ],
        },
      },
      // Code panels
      {
        type: 'div',
        props: {
          style: { display: 'flex', gap: 12, flex: 1 },
          children: [
            // Left panel: Go
            {
              type: 'div',
              props: {
                style: {
                  display: 'flex',
                  flexDirection: 'column',
                  flex: 1,
                  background: 'rgba(255, 172, 66, 0.08)',
                  borderRadius: 12,
                  padding: 12,
                },
                children: [
                  {
                    type: 'div',
                    props: {
                      style: { fontSize: 16, fontWeight: 600, marginBottom: 16, color: 'oklch(0.55 0.15 50)' },
                      children: 'Go Backend',
                    },
                  },
                  {
                    type: 'div',
                    props: {
                      style: {
                        display: 'flex',
                        flexDirection: 'column',
                        fontFamily: 'Roboto Mono',
                        fontSize: 14,
                        lineHeight: 1.5,
                        color: '#222',
                      },
                      children: goCode,
                    },
                  },
                ],
              },
            },
            // Right panel: TypeScript
            {
              type: 'div',
              props: {
                style: {
                  display: 'flex',
                  flexDirection: 'column',
                  flex: 1,
                  background: 'rgba(255, 172, 66, 0.08)',
                  borderRadius: 12,
                  padding: 12,
                },
                children: [
                  {
                    type: 'div',
                    props: {
                      style: { fontSize: 16, fontWeight: 600, marginBottom: 16, color: 'oklch(0.55 0.15 50)' },
                      children: 'TypeScript Client (manifest-driven ES6 proxy, no codegen!)',
                    },
                  },
                  {
                    type: 'div',
                    props: {
                      style: {
                        display: 'flex',
                        flexDirection: 'column',
                        fontFamily: 'Roboto Mono',
                        fontSize: 14,
                        lineHeight: 1.5,
                        color: '#222',
                      },
                      children: tsCode,
                    },
                  },
                ],
              },
            },
          ],
        },
      },
    ],
  },
};

const svg = await satori(markup, {
  width: 1200,
  height: 625,
  fonts: [
    {
      name: 'Noto Sans',
      data: fontData,
      weight: 400,
      style: 'normal',
    },
    {
      name: 'Noto Sans',
      data: fontBoldData,
      weight: 700,
      style: 'normal',
    },
    {
      name: 'Roboto Mono',
      data: fontMonoData,
      weight: 400,
      style: 'normal',
    },
    {
      name: 'Rubik Glitch',
      data: fontRubikGlitchData,
      weight: 400,
      style: 'normal',
    },
  ],
});

await fs.writeFile('tygor-banner.svg', svg);
console.log('✓ Generated tygor-banner.svg');
