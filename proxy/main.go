// Package main implementa o Chaos Proxy — o núcleo do laboratório go-to-chaos.
//
// Expõe duas portas:
//   - :8080 → Tráfego de proxy reverso (cliente → backend)
//   - :8081 → API administrativa para configurar o caos em tempo real
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// ============================================================
// GERENCIADOR DE CONFIGURAÇÃO DE CAOS (Thread-Safe)
// ============================================================

// ChaosConfig armazena as configurações de injeção de falhas.
// Cada campo representa um modo de caos independente.
type ChaosConfig struct {
	LatencyMs        int     `json:"latency_ms"`        // Latência a injetar em milissegundos (0 = desabilitado)
	ErrorProbability float64 `json:"error_probability"` // Probabilidade de retornar erro HTTP (0.0 a 1.0)
	TCPHijack        bool    `json:"tcp_hijack"`        // Abortar conexão TCP abruptamente
}

// ChaosManager gerencia o estado da configuração de caos de forma concorrente.
// Usa sync.RWMutex: múltiplas goroutines podem ler simultaneamente,
// mas somente uma pode escrever por vez (quando o admin atualiza a config).
type ChaosManager struct {
	mu     sync.RWMutex
	config ChaosConfig
}

// Get retorna uma cópia segura da configuração atual.
func (cm *ChaosManager) Get() ChaosConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.config
}

// Set atualiza a configuração com exclusão mútua total.
func (cm *ChaosManager) Set(cfg ChaosConfig) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config = cfg
	log.Printf("[PROXY] Configuração de caos atualizada: latency=%dms | error=%.0f%% | hijack=%v",
		cfg.LatencyMs, cfg.ErrorProbability*100, cfg.TCPHijack)
}

// ============================================================
// SERVIDOR PRINCIPAL
// ============================================================

func main() {
	// URL do backend de destino
	targetURL, err := url.Parse("http://backend:3000")
	if err != nil {
		log.Fatalf("[PROXY] URL do backend inválida: %v", err)
	}

	manager := &ChaosManager{}

	// Configuração inicial (sem caos)
	manager.Set(ChaosConfig{LatencyMs: 0, ErrorProbability: 0.0, TCPHijack: false})

	// Goroutine 1: Servidor Admin na porta :8081
	go startAdminServer(manager)

	// Goroutine 2: Servidor de Proxy na porta :8080
	startProxyServer(targetURL, manager)
}

// ============================================================
// SERVIDOR ADMINISTRATIVO (Porta :8081)
// ============================================================

// startAdminServer inicia a API de controle de caos em tempo real.
func startAdminServer(manager *ChaosManager) {
	mux := http.NewServeMux()

	// POST /config → Atualiza a configuração de caos
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido. Use POST.", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Erro ao ler corpo da requisição.", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var cfg ChaosConfig
		if err := json.Unmarshal(body, &cfg); err != nil {
			http.Error(w, fmt.Sprintf("JSON inválido: %v", err), http.StatusBadRequest)
			return
		}

		manager.Set(cfg)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","message":"Configuração de caos aplicada com sucesso."}`)
	})

	// GET /status → Exibe a configuração atual
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		cfg := manager.Get()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfg)
	})

	addr := ":8081"
	log.Printf("[PROXY] API Admin iniciada na porta %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[PROXY] Falha na API Admin: %v", err)
	}
}

// ============================================================
// SERVIDOR DE PROXY REVERSO (Porta :8080)
// ============================================================

// startProxyServer inicia o proxy reverso com injeção de caos.
func startProxyServer(targetURL *url.URL, manager *ChaosManager) {
	// Configura o proxy reverso nativo do Go
	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// ModifyResponse: Intercepta a resposta do backend antes de enviá-la ao cliente.
	// Injeta um cabeçalho indicando que o proxy processou a requisição.
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("X-Chaos-Proxy", "go-to-chaos/v1")
		resp.Header.Set("X-Backend-Status", fmt.Sprintf("%d", resp.StatusCode))
		return nil
	}

	// ErrorHandler: Tratamento de erros de conexão com o backend.
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[PROXY] Erro de conexão com o backend: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"Backend indisponível","detail":"%v"}`, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", chaosMiddleware(proxy, manager))

	addr := ":8080"
	log.Printf("[PROXY] Chaos Proxy iniciado na porta %s → Backend: %s", addr, targetURL)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[PROXY] Falha no Proxy: %v", err)
	}
}

// ============================================================
// MIDDLEWARE DE CAOS (O Coração do Projeto)
// ============================================================

// chaosMiddleware intercepta cada requisição e decide qual modo de caos aplicar,
// ou se a requisição deve seguir normalmente para o backend.
func chaosMiddleware(next http.Handler, manager *ChaosManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := manager.Get()
		clientIP := r.RemoteAddr

		log.Printf("[PROXY] Requisição recebida — %s %s | IP: %s", r.Method, r.URL.Path, clientIP)

		// ─────────────────────────────────────────────────────────
		// MODO 1: HIJACK TCP — Fecha a conexão abruptamente
		// ─────────────────────────────────────────────────────────
		if cfg.TCPHijack {
			log.Printf("[PROXY] [CAOS] TCP Hijack ativo — encerrando conexão de %s abruptamente!", clientIP)

			hijacker, ok := w.(http.Hijacker)
			if !ok {
				log.Printf("[PROXY] Hijack não suportado pelo servidor HTTP atual.")
				http.Error(w, "Hijack não suportado", http.StatusInternalServerError)
				return
			}

			conn, _, err := hijacker.Hijack()
			if err != nil {
				log.Printf("[PROXY] Erro ao fazer hijack da conexão: %v", err)
				return
			}
			// Fecha o socket TCP raw — o cliente recebe "connection reset by peer"
			conn.Close()
			return
		}

		// ─────────────────────────────────────────────────────────
		// MODO 2: INJEÇÃO DE ERRO HTTP — Curto-circuito sem atingir o backend
		// ─────────────────────────────────────────────────────────
		if cfg.ErrorProbability > 0 && rand.Float64() < cfg.ErrorProbability {
			log.Printf("[PROXY] [CAOS] Erro injetado (prob=%.0f%%) — retornando HTTP 503 para %s",
				cfg.ErrorProbability*100, clientIP)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Chaos-Mode", "error-injection")
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"error":"Service Unavailable","chaos":"error-injection","probability":"%.0f%%"}`,
				cfg.ErrorProbability*100)
			return
		}

		// ─────────────────────────────────────────────────────────
		// MODO 3: LATÊNCIA ARTIFICIAL — Delay resiliente com Context
		// ─────────────────────────────────────────────────────────
		if cfg.LatencyMs > 0 {
			delay := time.Duration(cfg.LatencyMs) * time.Millisecond
			log.Printf("[PROXY] [CAOS] Injetando latência de %v para %s...", delay, clientIP)

			select {
			case <-time.After(delay):
				// Delay concluído normalmente — a requisição segue para o backend.
				log.Printf("[PROXY] Latência de %v concluída. Encaminhando para o backend.", delay)

			case <-r.Context().Done():
				// O CLIENTE DESISTIU ANTES DO DELAY TERMINAR!
				// Isso é o uso correto de context.Context em Go:
				// paramos o sleep imediatamente, liberando a goroutine e a memória.
				log.Printf("[PROXY] [CONTEXTO] Cliente %s cancelou a requisição durante o delay! Goroutine liberada.", clientIP)
				return
			}
		}

		// ─────────────────────────────────────────────────────────
		// FLUXO NORMAL — Encaminha para o backend via proxy reverso
		// ─────────────────────────────────────────────────────────
		log.Printf("[PROXY] Encaminhando requisição de %s para o backend...", clientIP)
		next.ServeHTTP(w, r)
	}
}
