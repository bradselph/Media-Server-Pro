import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import sonarjs from 'eslint-plugin-sonarjs'
import tseslint from 'typescript-eslint'
import {defineConfig, globalIgnores} from 'eslint/config'

export default defineConfig([
    globalIgnores(['dist']),
    {
        files: ['**/*.{ts,tsx}'],
        extends: [
            js.configs.recommended,
            tseslint.configs.recommended,
            reactHooks.configs.flat.recommended,
            reactRefresh.configs.vite,
            sonarjs.configs.recommended,
        ],
        languageOptions: {
            ecmaVersion: 2020,
            globals: globals.browser,
        },
        rules: {
            // ── SonarJS tuning ──
            'sonarjs/cognitive-complexity': ['warn', 25],
            'sonarjs/no-duplicate-string': ['warn', { threshold: 4 }],
            'sonarjs/no-identical-functions': 'warn',
            'sonarjs/no-collapsible-if': 'warn',
            'sonarjs/prefer-single-boolean-return': 'warn',
            'sonarjs/no-redundant-jump': 'warn',
            'sonarjs/no-small-switch': 'warn',
            'sonarjs/no-unused-collection': 'warn',
            'sonarjs/no-gratuitous-expressions': 'warn',
            'sonarjs/no-nested-template-literals': 'off',

            // ── TypeScript strictness ──
            '@typescript-eslint/no-explicit-any': 'warn',
            '@typescript-eslint/no-unused-vars': ['warn', {
                argsIgnorePattern: '^_',
                varsIgnorePattern: '^_',
            }],

            // ── General quality ──
            'no-console': ['warn', { allow: ['warn', 'error'] }],
            'no-debugger': 'error',
            'prefer-const': 'warn',
            'no-var': 'error',
            'eqeqeq': ['warn', 'always'],
        },
    },
])
