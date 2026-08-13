// Package main implementa o Backend de Mock (API Alvo) do laboratório go-to-chaos.
// Simula um serviço real respondendo a requisições na porta :3000.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Response representa a estrutura de resposta da API.
type Response struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", handleData)
	mux.HandleFunc("/health", handleHealth)

	addr := ":3000"
	log.Printf("[BACKEND] Servidor iniciado na porta %s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("[BACKEND] Falha ao iniciar servidor: %v", err)
	}
}

// handleData processa requisições ao endpoint principal de dados.
func handleData(w http.ResponseWriter, r *http.Request) {
	log.Printf("[BACKEND] Requisição recebida — Método: %s | IP: %s | Hora: %s",
		r.Method, r.RemoteAddr, time.Now().Format("15:04:05.000"))

	resp := Response{
		Status:    "success",
		Message:   "Dados processados com sucesso pelo backend real.",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[BACKEND] Erro ao serializar resposta: %v", err)
	}
}

// handleHealth responde ao health check do Docker Compose / Load Balancer.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, `{"status":"healthy"}`)
}
