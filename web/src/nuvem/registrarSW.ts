// O registro do service worker do app da nuvem (ADR-019, F5). Só existe no
// build da nuvem: o ESLint impede o build do PC de importar src/nuvem, e o
// bundle.test.ts confere que o bundle local não tem serviceWorker.
//
// Só no build de produção (no servidor de desenvolvimento do Vite não há
// /sw.js) e só no navegador que tem service worker. No load, para não disputar
// a rede com a abertura do app. updateViaCache 'none': o navegador confere o
// /sw.js no servidor a cada navegação, sem passar pelo cache HTTP. Se o registro
// falhar, nada muda: o app funciona sem ele (só não abre sem rede).
export function registrarSW(janela: Window = window): void {
  if (!import.meta.env.PROD || !('serviceWorker' in janela.navigator)) return
  janela.addEventListener('load', () => {
    janela.navigator.serviceWorker.register('/sw.js', { scope: '/', updateViaCache: 'none' }).catch(() => {})
  })
}
