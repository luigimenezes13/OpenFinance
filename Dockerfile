# Build multi-stágio: o binário sai de uma imagem com toolchain Go, e a
# imagem final não tem compilador, shell nem gerenciador de pacotes.
FROM golang:1.26-alpine AS builder

WORKDIR /src

# go.mod e go.sum primeiro, em camada própria: dependência muda muito menos
# que código, então o `go mod download` é reaproveitado do cache em toda build
# que só alterou fonte.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO desligado produz binário estático, que é o que permite a imagem final
# ser distroless (sem libc). -trimpath tira caminhos da máquina de build do
# binário; -s -w tiram tabela de símbolos e DWARF, reduzindo o tamanho.
#
# A build NÃO usa a tag devauth: o verificador de token falso não existe
# nesta imagem, e é isso que garante que ele não pode ser ativado em
# produção por configuração.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/api \
    ./cmd/api

# Distroless: sem shell, sem package manager, usuário não-root por default.
# Sem shell, um atacante que consiga execução de código não tem de onde
# encadear comandos.
FROM gcr.io/distroless/static-debian12:nonroot

# Certificados raiz vêm na imagem base — necessários pra validar o TLS do
# Google ao buscar as chaves públicas (JWKS) do verificador de ID token.
COPY --from=builder /out/api /api

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/api"]
