// Package main implementa o Cliente Resiliente do laboratório go-to-chaos.
//
// Faz requisições cíclicas ao backend via Chaos Proxy, demonstrando:
//   - Timeout estrito no http.Client (1 segundo)
//   - Retries com backoff linear em caso de falha
//   - Distinção de tipos de erro (timeout, 5xx, conexão abortada)
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	// proxyURL é o endereço do Chaos Proxy.
	// Em Docker Compose, usa o nome do serviço como hostname.
	proxyURL = "http://proxy:8080/api/data"

	// clientTimeout define o tempo máximo que o cliente aguarda por uma resposta.
	// Se o Proxy injetar uma latência maior que isso, o cliente cancela a requisição.
	clientTimeout = 1 * time.Second

	// maxRetries é o número máximo de tentativas em caso de falha.
	maxRetries = 3

	// retryDelay é o tempo de espera entre tentativas (backoff linear simples).
	retryDelay = 200 * time.Millisecond

	// requestInterval é o intervalo entre cada ciclo de requisição.
	requestInterval = 2 * time.Second
)

func main() {
	// Criamos um http.Client com timeout estrito.
	// Sem isso, o cliente poderia ficar bloqueado indefinidamente.
	client := &http.Client{
		Timeout: clientTimeout,
	}

	target := proxyURL
	// Permite sobrescrever a URL via variável de ambiente (útil fora do Docker)
	if envURL := os.Getenv("PROXY_URL"); envURL != "" {
		target = envURL
	}

	log.Printf("[CLIENTE] Iniciando cliente resiliente → Target: %s", target)
	log.Printf("[CLIENTE] Timeout: %v | MaxRetries: %d | Intervalo: %v",
		clientTimeout, maxRetries, requestInterval)

	// Loop principal: faz uma requisição a cada requestInterval
	contador := 0
	for {
		contador++
		log.Printf("[CLIENTE] ─────────────────────────────────────────────")
		log.Printf("[CLIENTE] Iniciando chamada #%d...", contador)

		resultado, tentativas, err := fazerRequisicaoComRetry(client, target)

		if err != nil {
			log.Printf("[CLIENTE] Chamada #%d FALHOU após %d tentativa(s): %v",
				contador, tentativas, err)
		} else {
			log.Printf("[CLIENTE] Chamada #%d BEM-SUCEDIDA na tentativa %d/%d → Resposta: %s",
				contador, tentativas, maxRetries, resultado)
		}

		time.Sleep(requestInterval)
	}
}

// fazerRequisicaoComRetry executa a requisição HTTP com lógica de retry.
// Retorna o corpo da resposta, o número de tentativas realizadas e o erro final (se houver).
func fazerRequisicaoComRetry(client *http.Client, url string) (string, int, error) {
	var ultimoErro error

	for tentativa := 1; tentativa <= maxRetries; tentativa++ {
		if tentativa > 1 {
			log.Printf("[CLIENTE] Aguardando %v antes da tentativa %d/%d...",
				retryDelay, tentativa, maxRetries)
			time.Sleep(retryDelay)
		}

		corpo, err := fazerRequisicao(client, url, tentativa)
		if err == nil {
			return corpo, tentativa, nil
		}

		ultimoErro = err

		// Analisa o tipo de erro para log contextualizado
		classificarErro(err, tentativa)
	}

	return "", maxRetries, fmt.Errorf("todas as %d tentativas falharam. Último erro: %w",
		maxRetries, ultimoErro)
}

// fazerRequisicao executa uma única requisição HTTP.
// Retorna o corpo como string ou um erro descritivo.
func fazerRequisicao(client *http.Client, url string, tentativa int) (string, error) {
	inicio := time.Now()

	resp, err := client.Get(url)
	if err != nil {
		// O erro pode ser: timeout, conexão recusada, TCP reset, etc.
		return "", fmt.Errorf("erro na requisição (tentativa %d): %w", tentativa, err)
	}
	defer resp.Body.Close()

	duracao := time.Since(inicio)

	// Tratamento de respostas HTTP de erro (4xx, 5xx)
	if resp.StatusCode >= http.StatusBadRequest {
		corpo, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("servidor retornou HTTP %d em %v: %s",
			resp.StatusCode, duracao, string(corpo))
	}

	corpo, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("erro ao ler resposta (tentativa %d): %w", tentativa, err)
	}

	log.Printf("[CLIENTE] Requisição concluída em %v", duracao)
	return string(corpo), nil
}

// classificarErro loga uma mensagem contextualizada com base no tipo de erro.
// Em Go, erros de rede são detectados via análise da string de erro,
// pois os tipos concretos estão em net.OpError e net.Error.
func classificarErro(err error, tentativa int) {
	errStr := err.Error()

	switch {
	case isTimeout(err):
		log.Printf("[CLIENTE] [TIMEOUT] Tentativa %d: O cliente atingiu o limite de %v. "+
			"O Chaos Proxy estava injetando latência maior que o timeout configurado.",
			tentativa, clientTimeout)
	case contains(errStr, "connection reset by peer"), contains(errStr, "EOF"):
		log.Printf("[CLIENTE] [TCP RESET] Tentativa %d: Conexão abortada abruptamente pelo proxy. "+
			"Isso é o modo TCP Hijack em ação!",
			tentativa)
	case contains(errStr, "connection refused"):
		log.Printf("[CLIENTE] [CONN REFUSED] Tentativa %d: Proxy offline ou porta errada.",
			tentativa)
	default:
		log.Printf("[CLIENTE] [ERRO] Tentativa %d: %v", tentativa, err)
	}
}

// isTimeout verifica se um erro é do tipo timeout de rede.
// Como fazerRequisicao embrulha o erro original com fmt.Errorf("...: %w", err),
// é preciso percorrer a cadeia de erros (errors.Unwrap) até achar o erro
// concreto (*url.Error) que implementa a interface Timeout() bool.
func isTimeout(err error) bool {
	type timeoutError interface {
		Timeout() bool
	}
	for unwrapped := err; unwrapped != nil; unwrapped = errors.Unwrap(unwrapped) {
		if te, ok := unwrapped.(timeoutError); ok {
			return te.Timeout()
		}
	}
	return false
}

// contains é um helper para verificar substrings em mensagens de erro.
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
