// Pengaman regresi wiring server: rute rekonsiliasi bank (StatementMatchPost)
// memakai h.Pool untuk transaksi jurnal selisih. Bila server dibangun dengan
// handlers.New (tanpa pool), match dengan selisih selalu 500
// "pool database tidak tersedia" di server nyata, sementara smoketest tetap
// hijau karena ia memanggil NewWithPool sendiri. Test ini memastikan
// cmd/server/main.go mengonsruksi handler dengan pool.
package main

import (
	"os"
	"strings"
	"testing"
)

func TestServerWiringUsesPool(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("baca main.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, "handlers.NewWithPool(queries, pool,") {
		t.Fatal("cmd/server harus membangun handler dengan handlers.NewWithPool(queries, pool, ...); " +
			"handlers.New tanpa pool membuat rute match-selisih bank selalu 500")
	}
	if strings.Contains(s, "handlers.New(") {
		t.Fatal("handlers.New (tanpa pool) tidak boleh dipakai di cmd/server")
	}
}
