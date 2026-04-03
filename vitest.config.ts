import { resolve } from 'node:path';

import { defineConfig } from 'vitest/config';
import tsconfigPaths from 'vite-tsconfig-paths';

export default defineConfig({
  resolve: {
    alias: {
      '@web/shared': resolve(__dirname, 'libs/web/shared/src/index.ts'),
      '@web/shared/': `${resolve(__dirname, 'libs/web/shared/src')}/`,
      '@api/user': resolve(__dirname, 'libs/api/user/src/index.ts'),
      '@api/user/': `${resolve(__dirname, 'libs/api/user/src')}/`,
      '@shared/types': resolve(__dirname, 'libs/shared/types/src/index.ts'),
      '@shared/types/': `${resolve(__dirname, 'libs/shared/types/src')}/`,
    },
  },
  plugins: [
    tsconfigPaths({
      projects: ['tsconfig.base.json'],
      ignoreConfigErrors: true,
    }),
  ],
  test: {
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    include: ['libs/**/*.spec.ts'],
    passWithNoTests: false,
  },
});
