import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';

export default defineConfig({
  site: 'https://enigma-brain.github.io',
  base: '/axon/',
  integrations: [
    starlight({
      title: 'axon',
      description: 'Documentation for axon, the command-line entry point to the platform.',
      plugins: [
        // Fails the build on a broken internal link.
        starlightLinksValidator({ errorOnRelativeLinks: true }),
      ],
      sidebar: [
        {
          label: 'Start here',
          items: [
            { label: 'Installing', slug: 'installing' },
            { label: 'Logging in', slug: 'logging-in' },
          ],
        },
        {
          label: 'Command groups',
          items: [
            {
              label: 'devenv',
              items: [
                { label: 'Overview', slug: 'devenv' },
                { label: 'Backend: SkyPilot', slug: 'devenv/skypilot' },
              ],
            },
          ],
        },
        {
          label: 'Pathway kinds',
          items: [
            { label: 'Direct', slug: 'pathways/direct' },
            { label: 'Teleport', slug: 'pathways/teleport' },
          ],
        },
        {
          label: 'For organisations',
          items: [{ label: 'Writing an organisation file', slug: 'organisation-file' }],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Files on your laptop', slug: 'files' },
            { label: 'Troubleshooting', slug: 'troubleshooting' },
          ],
        },
      ],
    }),
  ],
});
