package kernel

// Page é a janela de uma consulta paginada: quantos itens e a partir de
// onde. Building block do shared kernel porque paginação não é conceito de
// nenhum bounded context em particular.
//
// Existe como VO, e não como dois ints soltos na assinatura do repositório,
// por duas razões: `List(ctx, userID, 50, 100)` não diz qual número é qual,
// e o limite MÁXIMO precisa morar em algum lugar — sem ele, um cliente pede
// `limit=1000000` e derruba o serviço com uma requisição.
//
// DDD: Value Object — imutável, auto-validado.
type Page struct {
	limit  int
	offset int
}

// Limites da paginação. DefaultLimit é o que vale quando o cliente não pede
// nada; MaxLimit é o teto que ele não pode furar.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// NewPage valida e constrói a janela. Limite ZERO significa "não pedi nada",
// e recebe o default — é diferente de limite negativo, que é pedido
// inválido.
//
// Limite acima do teto é ERRO, não corte silencioso: quem pediu 1000 e
// recebeu 200 sem aviso pagina errado e perde registros sem perceber.
func NewPage(limit int, offset int) (Page, error) {
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 0 || limit > MaxLimit {
		return Page{}, ErrInvalidPage
	}
	if offset < 0 {
		return Page{}, ErrInvalidPage
	}
	return Page{limit: limit, offset: offset}, nil
}

// Limit retorna quantos itens a janela pede.
func (p Page) Limit() int {
	return p.limit
}

// Offset retorna a partir de qual item a janela começa.
func (p Page) Offset() int {
	return p.offset
}

// IsZero informa se esta Page foi criada fora do construtor.
func (p Page) IsZero() bool {
	return p.limit == 0
}
