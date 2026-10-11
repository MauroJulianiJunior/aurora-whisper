import js from '@eslint/js'
import { globalIgnores } from 'eslint/config'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import globals from 'globals'
import tseslint from 'typescript-eslint'

// Os dois builds do painel (ADR-019, decisão 12) não se misturam: o código
// da nuvem (src/nuvem) só é importado de dentro dela, e ela não importa o que
// é só do PC (o bilhete, o cliente da Local API, as Configurações do PC, o
// aprendizado). bundle.test.ts confere o resultado nos dois bundles.
const daNuvem = {
  group: ['**/nuvem', '**/nuvem/**', './nuvem', './nuvem/**'],
  message: 'O código da nuvem só entra no build da nuvem (src/nuvem; ADR-019, decisão 12).',
}

// soDoPC vale em qualquer profundidade: '../clienteLocal' de src/nuvem e
// '../../clienteLocal' de src/nuvem/sub. Só o que sobe de pasta (../): o que
// a nuvem importa de dentro dela (./) não é do PC.
const modulosDoPC = [
  'App',
  'ambienteLocal',
  'aprendizado',
  'AprendidoNoPainel',
  'clienteLocal',
  'config',
  'Configuracoes',
  'FaixaLocal',
  'main',
  'sessao',
  'Termos',
]
const soDoPC = {
  regex: `^(?:\\.\\./)+(?:${modulosDoPC.join('|')})(?:\\.tsx?)?$`,
  caseSensitive: true,
  message: 'Isto é só do PC: não vai para o build da nuvem (ADR-019, decisão 12).',
}

export default tseslint.config(
  globalIgnores(['dist', 'dist-nuvem']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat['recommended-latest'],
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
  },
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['src/nuvem/**'],
    rules: { 'no-restricted-imports': ['error', { patterns: [daNuvem] }] },
  },
  {
    files: ['src/nuvem/**/*.{ts,tsx}'],
    rules: { 'no-restricted-imports': ['error', { patterns: [soDoPC] }] },
  },
  // O service worker do app da nuvem (ADR-019, F5): um script clássico, que o
  // build copia com os marcadores trocados. Roda no escopo do service worker.
  {
    files: ['pwa/sw.js'],
    extends: [js.configs.recommended],
    languageOptions: { ecmaVersion: 2020, sourceType: 'script', globals: globals.serviceworker },
  },
)
