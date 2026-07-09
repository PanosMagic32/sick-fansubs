import { defineConfig } from 'vitest/config';
import tsconfigPaths from 'vite-tsconfig-paths';

export default defineConfig({
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
