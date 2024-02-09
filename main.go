package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	pq "github.com/lib/pq"
)

var db *sql.DB
var stmt *sql.Stmt

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists { return value }
	return fallback
}

func main() {
	var err error

	port := getEnv("PORT", "8080")
	host := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	user := getEnv("DB_USER", "postgres")
	password := getEnv("DB_PASSWORD", "postgres")
	database := getEnv("DB_NAME", "postgres")
	sslmode := getEnv("DB_SSLMODE", "disable")

	connstr := fmt.Sprintf("host=%s port=%s user=%s password=%s database=%s sslmode=%s", host, dbPort, user, password, database, sslmode)
	db, err = sql.Open("postgres", connstr)
	if err != nil { log.Fatal(err) }
	defer db.Close()

	stmt, err = db.Prepare(`
		select json_build_object(
			'saldo', json_build_object(
			'total', (select saldo from contas where id = $1),
			'data_extrato', now(),
			'limite', (select limite from contas where id = $1)
			),
			'ultimas_transacoes', coalesce((
				select json_agg(json_build_object(
					'valor', valor,
					'tipo', tipo,
					'descricao', descricao,
					'realizada_em', realizada_em
					)) 
				from (
					select valor, tipo, descricao, realizada_em
					from transacoes
					where conta_id = $1
					order by realizada_em desc
					limit 10
				) as last_transactions
			), '[]'::json)
			)
		from contas
		where id = $1;
	`)
	if err != nil { log.Fatal(err) }
	defer stmt.Close()

	http.HandleFunc("/clientes/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/") // split URL path
		if len(parts) != 4 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		rawId := parts[2] // get id from URL
		id, err := strconv.Atoi(rawId) // parse id to int
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}

		if r.Method == http.MethodGet && parts[3] == "extrato" {
			getExtrato(w, id)
		} else if r.Method == http.MethodPost && parts[3] == "transacoes" {
			postTransacao(w, id, r)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	fmt.Printf("server running on port %s\n", port)
	log.Fatal(http.ListenAndServe(":" + port, nil))
}

func getExtrato(w http.ResponseWriter, id int) {
	var result []byte
	err := stmt.QueryRow(&id).Scan(&result)
	if err != nil {
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(result)
}

type transactionRequest struct {
	Tipo        string `json:"tipo"`
	Valor       int    `json:"valor"`
	Descricao   string `json:"descricao"`
}

func postTransacao(w http.ResponseWriter, id int, r *http.Request) {
	body, err := io.ReadAll(r.Body) // read request body
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	var req transactionRequest
	err = json.Unmarshal(body, &req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	if (req.Tipo != "c" && req.Tipo != "d") || (len(req.Descricao) < 1 || len(req.Descricao) > 10) { // validate body
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	var result []byte
	tx, err := db.Begin()
	err = tx.QueryRow("select process($1, $2, $3, $4)", id, req.Valor, req.Descricao, req.Tipo).Scan(&result)

	if err != nil {
		tx.Rollback()
		if pgErr, ok := err.(*pq.Error); ok {
			switch pgErr.Code {
			case "23000":
				w.WriteHeader(http.StatusUnprocessableEntity)
			case "23503":
				w.WriteHeader(http.StatusNotFound)
			default:
				w.WriteHeader(http.StatusInternalServerError)
			}
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	} else {
		tx.Commit()
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write(result)
	}
}

