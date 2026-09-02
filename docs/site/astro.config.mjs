// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLlmsTxt from 'starlight-llms-txt';

// https://astro.build/config
export default defineConfig({
	// `starlight-llms-txt` needs an absolute `site` to build the URLs it lists
	// in llms.txt/llms-full.txt (Astro also uses it for the sitemap).
	// The docs are served from the ergonlabs marketing site, one path segment
	// per product, so `base` has to match where the built `dist/` is copied to.
	site: 'https://ergonlabs.io',
	base: '/docs/omni',
	integrations: [
		starlight({
			title: 'omni',
			description: 'Run any coding agent CLI through a controlled interception proxy.',
			// Overrides Starlight's theme tokens only (accent, greys, fonts) so
			// these docs match ergonlabs.io, which serves them.
			customCss: ['./src/styles/custom.css'],
			// No `social` entry: the repo has no public GitHub home yet. Add one
			// here once it does.
			plugins: [
				// Generates /llms.txt and /llms-full.txt at build time -- the
				// reason this project uses Starlight instead of a Go-native doc
				// generator. See docs/site/README.md.
				starlightLlmsTxt(),
			],
			// The sidebar spine follows the shape of the product, not the shape
			// of the implementation plan. A sidebar entry that points at a page
			// that doesn't exist fails the build, so a group (or an item within
			// it) stays commented out until its page lands. Uncomment an item
			// (and add its page under src/content/docs/<slug>/) as content
			// arrives; keep the group order and item labels as written.
			sidebar: [
				// Way back to the site that serves these docs. External links are
				// left untouched by `base`, so this one is written in full.
				{ label: '← ergonlabs.io', link: 'https://ergonlabs.io/' },
				{
					label: 'Getting started',
					items: [
						{ label: 'Introduction', slug: 'getting-started/introduction' },
						{ label: 'Installation', slug: 'getting-started/installation' },
						{ label: 'Quickstart', slug: 'getting-started/quickstart' },
					],
				},
				{
					label: 'Configuration',
					items: [
						{ label: 'Configuration file', slug: 'configuration/configuration-file' },
						{ label: 'API keys and credentials', slug: 'configuration/credentials' },
						{ label: 'Per-project config', slug: 'configuration/per-project-config' },
						{ label: 'Environment variables', slug: 'configuration/environment-variables' },
					],
				},
				{
					label: 'Interception',
					items: [
						{ label: 'How interception works', slug: 'interception/how-it-works' },
						{ label: 'Model routing', slug: 'interception/model-routing' },
						{ label: 'All-traffic mode and the CA', slug: 'interception/all-traffic' },
					],
				},
				{
					label: 'Agents',
					items: [
						{ label: 'Claude Code', slug: 'agents/claude-code' },
						{ label: 'Codex', slug: 'agents/codex' },
						{ label: 'Custom profiles', slug: 'agents/custom-profiles' },
					],
				},
				{
					label: 'Sessions',
					items: [
						{ label: 'Recorded sessions', slug: 'sessions/recorded-sessions' },
						{ label: 'Redaction', slug: 'sessions/redaction' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'CLI', slug: 'reference/cli' },
						{ label: 'Config schema', slug: 'reference/config-schema' },
						{ label: 'Troubleshooting', slug: 'reference/troubleshooting' },
					],
				},
			],
		}),
	],
});
