# 🌀 go-to-chaos

> **Laboratório de Chaos Engineering em Go** — Um proxy reverso que injeta falhas de rede de forma controlada para testar a resiliência de microserviços.

Criado como parte de um post técnico para a comunidade HunCoding. O projeto demonstra como `net/http`, `httputil.ReverseProxy`, `context.Context` e `sync.RWMutex` se combinam para construir ferramentas de engenharia de confiabilidade robustas em Go.

---

## 🏗️ Arquitetura

```
[Cliente Resiliente] → :8080 → [Chaos Proxy] → :3000 → [Backend Mock]
                                     ↑
                              API Admin :8081
                           (Controle do caos)
```

### Serviços

| Serviço   | Porta  | Descrição                                               |
|-----------|--------|---------------------------------------------------------|
| `backend` | `3000` | API alvo mock. Simula o serviço real de produção.       |
| `proxy`   | `8080` | Chaos Proxy. Intercepta e injeta falhas nas requisições.|
| `proxy`   | `8081` | API Admin. Controla o modo de caos em tempo real.       |
| `client`  | —      | Cliente com timeout estrito e lógica de retry.          |

---

## 🚀 Como rodar

### Pré-requisitos
- Docker Desktop ou Docker Engine + Docker Compose v2
- Go 1.22+ (apenas para desenvolvimento local sem Docker)

### Subir o ambiente completo

```bash
# Clonar o repositório
git clone https://github.com/jorgegabrielti/go-to-chaos.git
cd go-to-chaos

# Build e start de todos os containers
docker compose up --build

# Em outra janela, monitorar logs por serviço
docker compose logs -f proxy    # Logs do proxy (mostra o caos aplicado)
docker compose logs -f client   # Logs do cliente (mostra timeouts, retries)
docker compose logs -f backend  # Logs do backend (mostra requisições reais)
```

---

## 🧪 Roteiro de Laboratório (Passo a Passo)

### ✅ Lab 1 — Fluxo Feliz (Sem Caos)

O ambiente inicia sem nenhuma configuração de caos. Verifique que tudo funciona normalmente.

**Observar nos logs do client:**
```
[CLIENTE] ✅ Chamada #1 BEM-SUCEDIDA na tentativa 1/3 → Resposta: {"status":"success",...}
[CLIENTE] ⚡ Requisição concluída em 2ms
```

**Verificar o status atual do proxy:**

```bash
# Linux / macOS
curl http://localhost:8081/status
```
```powershell
# Windows PowerShell
Invoke-RestMethod http://localhost:8081/status
```

---

### ⏳ Lab 2 — Latência e Cancelamento de Contexto

Injeta 2 segundos de latência. O cliente tem timeout de 1 segundo, então ele vai cancelar.

**Ativar caos de latência:**

```bash
# Linux / macOS
curl -X POST http://localhost:8081/config \
  -H "Content-Type: application/json" \
  -d '{"latency_ms": 2000, "error_probability": 0, "tcp_hijack": false}'
```
```powershell
# Windows PowerShell
Invoke-RestMethod -Method Post -Uri "http://localhost:8081/config" `
  -ContentType "application/json" `
  -Body '{"latency_ms": 2000, "error_probability": 0, "tcp_hijack": false}'
```

**Observar nos logs do proxy** (prova do cancelamento de contexto):
```
[PROXY] ⏳ [CAOS] Injetando latência de 2s para 172.18.0.4:xxxx...
[PROXY] 🛑 [CONTEXTO] Cliente cancelou a requisição durante o delay! Goroutine liberada.
```

**Observar nos logs do client:**
```
[CLIENTE] ⚠️  [ERRO] Tentativa 1: context deadline exceeded (Client.Timeout exceeded)
[CLIENTE] ⏳ Aguardando 200ms antes da tentativa 2/3...
[CLIENTE] ⚠️  [ERRO] Tentativa 2: context deadline exceeded (Client.Timeout exceeded)
[CLIENTE] ❌ Chamada #N FALHOU após 3 tentativa(s)
```

> 💡 **O pulo do gato:** Sem o `select { case <-r.Context().Done() }`, a goroutine do proxy ficaria presa em `time.Sleep(2s)` consumindo memória mesmo após o cliente ir embora. Com o context, ela é liberada instantaneamente.

**Desativar caos:**

```bash
# Linux / macOS
curl -X POST http://localhost:8081/config \
  -H "Content-Type: application/json" \
  -d '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": false}'
```
```powershell
# Windows PowerShell
Invoke-RestMethod -Method Post -Uri "http://localhost:8081/config" `
  -ContentType "application/json" `
  -Body '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": false}'
```

---

### 🔥 Lab 3 — Injeção de Erros e Retries

50% das requisições retornarão HTTP 503. O cliente tentará até 3 vezes antes de desistir.

**Ativar caos de erro:**

```bash
# Linux / macOS
curl -X POST http://localhost:8081/config \
  -H "Content-Type: application/json" \
  -d '{"latency_ms": 0, "error_probability": 0.5, "tcp_hijack": false}'
```
```powershell
# Windows PowerShell
Invoke-RestMethod -Method Post -Uri "http://localhost:8081/config" `
  -ContentType "application/json" `
  -Body '{"latency_ms": 0, "error_probability": 0.5, "tcp_hijack": false}'
```

**Observar nos logs do client:**
```
[CLIENTE] ⚠️  [ERRO] Tentativa 1: servidor retornou HTTP 503: {"error":"Service Unavailable",...}
[CLIENTE] ⏳ Aguardando 200ms antes da tentativa 2/3...
[CLIENTE] ✅ Chamada #N BEM-SUCEDIDA na tentativa 2/3
```

> 💡 **Perguntas para refletir:** O seu serviço real tem um Circuit Breaker? Ao configurar `error_probability: 1.0` (100% de falha), o cliente faz 3 retries simultâneos — imagine 1.000 instâncias fazendo isso. Isso é um **Thundering Herd** contra o backend quando ele voltar!

**Desativar caos:**

```bash
# Linux / macOS
curl -X POST http://localhost:8081/config \
  -H "Content-Type: application/json" \
  -d '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": false}'
```
```powershell
# Windows PowerShell
Invoke-RestMethod -Method Post -Uri "http://localhost:8081/config" `
  -ContentType "application/json" `
  -Body '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": false}'
```

---

### 💀 Lab 4 — TCP Hijack (Falha Catastrófica)

O proxy fecha o socket TCP abruptamente. O cliente recebe `connection reset by peer` ou `EOF` sem nenhum cabeçalho HTTP.

**Ativar TCP Hijack:**

```bash
# Linux / macOS
curl -X POST http://localhost:8081/config \
  -H "Content-Type: application/json" \
  -d '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": true}'
```
```powershell
# Windows PowerShell
Invoke-RestMethod -Method Post -Uri "http://localhost:8081/config" `
  -ContentType "application/json" `
  -Body '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": true}'
```

**Observar nos logs do proxy:**
```
[PROXY] 💀 [CAOS] TCP Hijack ativo — encerrando conexão de 172.18.0.4:xxxx abruptamente!
```

**Observar nos logs do client:**
```
[CLIENTE] 💀 [TCP RESET] Tentativa 1: Conexão abortada abruptamente pelo proxy.
[CLIENTE] 💀 [TCP RESET] Tentativa 2: Conexão abortada abruptamente pelo proxy.
[CLIENTE] ❌ Chamada #N FALHOU após 3 tentativa(s)
```

**Desativar caos (restaurar estado limpo):**

```bash
# Linux / macOS
curl -X POST http://localhost:8081/config \
  -H "Content-Type: application/json" \
  -d '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": false}'
```
```powershell
# Windows PowerShell
Invoke-RestMethod -Method Post -Uri "http://localhost:8081/config" `
  -ContentType "application/json" `
  -Body '{"latency_ms": 0, "error_probability": 0, "tcp_hijack": false}'
```


---

## 📁 Estrutura do Projeto

```
go-to-chaos/
├── go.mod                  # Módulo Go
├── docker-compose.yml      # Orquestração dos containers
├── README.md               # Este arquivo
├── backend/
│   ├── Dockerfile
│   └── main.go             # API mock de destino
├── proxy/
│   ├── Dockerfile
│   └── main.go             # Chaos Proxy + API Admin
└── client/
    ├── Dockerfile
    └── main.go             # Cliente resiliente com retries
```

---

## 🛠️ Desenvolvimento Local (sem Docker)

```bash
# Terminal 1: Backend
go run ./backend/

# Terminal 2: Proxy (apontando para localhost)
# Altere a targetURL no proxy/main.go para http://localhost:3000
go run ./proxy/

# Terminal 3: Cliente (apontando para localhost)
PROXY_URL=http://localhost:8080/api/data go run ./client/
```

---

## 📚 Conceitos de Go Demonstrados

| Conceito                        | Onde                         | Por quê importa                                        |
|---------------------------------|------------------------------|--------------------------------------------------------|
| `net/http/httputil.ReverseProxy`| `proxy/main.go`              | Proxy reverso nativo, sem dependências externas        |
| `sync.RWMutex`                  | `ChaosManager.Get()/Set()`   | Leitura concorrente segura da config entre goroutines  |
| `context.Context` + `select`    | `chaosMiddleware`            | Cancela operações longas quando o cliente desiste      |
| `http.Hijacker`                 | Lab 4 (TCP Hijack)           | Acesso direto ao socket TCP subjacente                 |
| `http.Client` com `Timeout`     | `client/main.go`             | Garante que o cliente nunca bloqueia indefinidamente   |
| Error wrapping (`%w`)           | `fazerRequisicao`            | Preserva a cadeia de erros para diagnóstico            |

---

## 👤 Autor

**Jorge Gabriel Moraes Romero** — SRE com 10 anos de experiência em TI, especialista em AWS, Kubernetes, Terraform e Observabilidade.

---

## 📄 Licença

MIT License — use, modifique e compartilhe à vontade.
