// Package update implementa o verificador de update do lado do CLIENTE
// (Windows Agent), conforme docs/ai-spec/06-api-contract.md ("Updates"),
// docs/ai-spec/18-tool-contract.md ("UpdateProvider"),
// docs/ai-spec/12-rollout.md ("Feature/config flags" → auto_update_install)
// e docs/ai-spec/09-security-privacy.md (THR-010).
//
// O endpoint HTTP correspondente (GET /updates/windows/latest) já existe
// em internal/server — este pacote não o toca; ele só fala com ele pela
// rede, como qualquer outro cliente.
//
// # Responsabilidade
//
// Consultar o endpoint cloud de update, decidir LOCALMENTE (nunca confiar
// cegamente no servidor) se existe de fato uma versão mais nova, baixar o
// pacote anunciado e verificar seu SHA-256 antes de considerá-lo
// utilizável. Este pacote NUNCA instala nada sozinho (AC-039): ele entrega,
// no máximo, um VerifiedUpdate "pronto para instalar, aguardando
// confirmação".
//
// # Fluxo
//
//  1. CheckForUpdate (check.go) chama UpdateProvider.Latest e decide o
//     State: StateNoUpdate ou StateAvailable. A comparação de versão
//     (version.go) é semântica — nunca lexicográfica ("0.10.0" > "0.9.0")
//     — e nunca recomenda downgrade, mesmo que o servidor sugira uma
//     versão igual ou menor que a instalada.
//  2. Downloader.DownloadAndVerify (download.go) baixa o pacote anunciado
//     para um arquivo temporário e confere SHA-256 (THR-010) e tamanho
//     antes de devolver um VerifiedUpdate. Qualquer divergência de hash ou
//     de tamanho apaga o arquivo temporário e devolve erro — o arquivo
//     rejeitado nunca fica acessível como se fosse válido.
//  3. Install (install.go) é o único ponto de contato com um Installer
//     real, e só repassa a chamada quando confirmed=true (AC-039). O flag
//     auto_update_install (12-rollout.md) é off por padrão e é decisão de
//     configuração LOCAL de quem chama Install — este pacote nem carrega
//     esse campo, e a resposta do servidor não tem como ligá-lo (o único
//     jeito de confirmed virar true é o chamador — configuração local ou
//     confirmação interativa do usuário — decidir isso).
//
// # AC-040 — nenhum secret de provider no desktop
//
// UpdateProvider fala apenas com o endpoint cloud
// (GET /updates/windows/latest); é o backend quem abstrai se o release é
// público ou privado (06-api-contract.md, "Updates": "Isso abstrai GitHub
// público/privado"). Este pacote não declara nenhum campo, constante ou
// config de token/chave de provider — security_test.go varre o
// código-fonte do pacote (via go/ast) para garantir isso em CI.
package update
