package ginmiddleware

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/luigimenezes13/financial-manager/internal/identity/application"
)

// UserResolverFrom constrói o resolvedor sobre o use case de sign-in.
//
// É a cola entre a borda HTTP e a application: o middleware não conhece
// SignInUseCase e o use case não conhece gin — esta função de três linhas é
// o único ponto onde os dois se encostam.
//
// CUSTO CONSCIENTE: o sign-in roda em TODA requisição, e ele faz uma
// consulta ao banco (busca por identidade externa). Verificar o token é
// local e barato (chave pública em cache), mas a consulta não é. Duas saídas
// quando isso incomodar: cache de (subject → uuid) em memória com TTL, ou
// emitir um token próprio no primeiro login e passar a validar só ele. A
// segunda é a que escala, e não muda nada acima desta linha — motivo pra não
// antecipar nenhuma das duas agora.
func UserResolverFrom(signIn *application.SignInUseCase) ResolveUser {
	return func(ctx context.Context, rawToken string) (uuid.UUID, error) {
		output, err := signIn.Execute(ctx, application.SignInInput{RawToken: rawToken})
		if err != nil {
			return uuid.Nil, err
		}

		userID, err := uuid.Parse(output.UserID)
		if err != nil {
			// Inalcançável na prática (o uuid nasce do domínio), mas
			// engolir com uuid.Nil deixaria o request seguir como um
			// usuário que não existe.
			return uuid.Nil, fmt.Errorf("ginmiddleware: use case devolveu uuid inválido: %w", err)
		}
		return userID, nil
	}
}
