//go:build !logindeteste

package server

// Sem a build tag logindeteste (o build normal e o de produção), o login de
// teste não existe: nenhuma rota, e o GET e o POST /auth/teste caem no 404 das
// rotas não montadas. Ver login_de_teste.go.

// LoginDeTesteCompilado diz se este binário tem o login de teste.
const LoginDeTesteCompilado = false

func (c *Cloud) rotasDeTeste() []rota { return nil }
