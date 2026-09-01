# omni docs site

Astro + Starlight, same framework as the llm-proxy docs. This directory is
infrastructure; content pages live under `src/content/docs/`.

The built site is served from the ergonlabs marketing site at
`ergonlabs.io/docs/omni/` — one path segment per product. `base` in
`astro.config.mjs` must match that path, and the marketing repo's
`build-docs.sh` is what stages `dist/` there.

## Commands

From the repo root:

```
make docs-dev      # dev server at localhost:4321
make docs-build    # production build to docs/site/dist/
```

Or from this directory:

| Command         | Action                                 |
| :-------------- | :------------------------------------- |
| `npm install`   | Install dependencies                   |
| `npm run dev`   | Start local dev server                 |
| `npm run build` | Build the production site to `./dist/` |
| `npm run preview` | Preview a build locally              |

## Notable configuration

- **`starlight-llms-txt`** is enabled in `astro.config.mjs`. It generates
  `dist/llms.txt` and `dist/llms-full.txt` on every build — the reason this
  site uses Astro/Starlight rather than a Go-native doc generator.
- **Pagefind** search is Starlight's default and needs no setup; it indexes at
  build time into `dist/pagefind/`.
- **The sidebar spine is specified but partly commented out** in
  `astro.config.mjs`. An uncommented entry pointing at a page that does not
  exist fails the build, so a group stays commented until its first real page
  lands.
- **`src/styles/custom.css`** overrides Starlight's theme tokens only — accent,
  greys, fonts — so these docs match ergonlabs.io and a Starlight upgrade
  cannot silently break the look.

## Writing pages

Frontmatter carries `title`, `summary`, `last_updated`, and `related`.

Write links to other pages **relative** (`../reference/cli/`), never
root-absolute (`/reference/cli/`). Astro's `base` prefixes the links it
generates, but not root-absolute links written inside markdown — those ship
as-is and 404 under the `/docs/omni` base path.

Document what the code does today. This project is in Phase 0 and it is
better to have four accurate pages than twelve aspirational ones.

Check out [Starlight's docs](https://starlight.astro.build/) or
[the Astro documentation](https://docs.astro.build) for more.
