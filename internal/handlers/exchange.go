package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"accounting/internal/database/db"
	"accounting/internal/exchange"
	appmw "accounting/internal/middleware"
	"accounting/internal/models"
	"accounting/templates"
)

// exchangeDir menyimpan berkas unggahan sementara untuk import. Dapat diatur
// lewat EXCHANGE_DIR; bawaan menumpang direktori kerja layanan.
func exchangeDir() string {
	if dir := strings.TrimSpace(os.Getenv("EXCHANGE_DIR")); dir != "" {
		return dir
	}
	return filepath.Join("var", "exchange")
}

// dataset adalah pilihan data yang bisa diekspor (dan sebagian diimpor).
type dataset struct {
	Value      string
	Label      string
	Desc       string
	Importable bool
}

func datasets() []dataset {
	return []dataset{
		{"jurnal", "Jurnal (header dan baris)", "Seluruh jurnal beserta baris debit/kredit. Dapat juga diimpor.", true},
		{"akun", "Daftar akun (chart of accounts)", "Kode, nama, jenis, sub-jenis, saldo normal, dan pos PSAK 201. Dapat juga diimpor.", true},
		{"buku_besar", "Buku besar per akun", "Seluruh mutasi diurutkan per kode akun, siap ditelusuri di aplikasi lain.", false},
		{"neraca_saldo", "Neraca saldo", "Saldo debit dan kredit tiap akun sampai tanggal akhir.", false},
	}
}

func datasetInfo(value string) (dataset, bool) {
	for _, d := range datasets() {
		if d.Value == value {
			return d, true
		}
	}
	return dataset{}, false
}

// ExchangeGet menampilkan halaman export dan import.
func (h *Handlers) ExchangeGet(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	semua, bisaImpor := exchangeDatasets()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.ExchangePage(h.newShell(r, user, company, role, "exchange", "Export / Import "+company.Name),
		company, templates.ExchangeView{
			Datasets:   semua,
			Importable: bisaImpor,
			Formats:    exchangeFormatViews(),
			TodayLabel: models.FormatDateShort(time.Now()),
		}).Render(r.Context(), w)
}

// exchangeDatasets menyiapkan pilihan data beserta flag bisa-diimpor.
func exchangeDatasets() (semua, bisaImpor []templates.ExchangeDataset) {
	for _, d := range datasets() {
		v := templates.ExchangeDataset{Value: d.Value, Label: d.Label, Desc: d.Desc, Importable: d.Importable}
		semua = append(semua, v)
		if d.Importable {
			bisaImpor = append(bisaImpor, v)
		}
	}
	return semua, bisaImpor
}

func exchangeFormatViews() []templates.ExchangeFormat {
	formats := exchange.AllFormats()
	out := make([]templates.ExchangeFormat, 0, len(formats))
	for _, f := range formats {
		out = append(out, templates.ExchangeFormat{Value: f.Value, Label: f.Label, Ext: f.Ext})
	}
	return out
}

// ExchangeExport mengirim berkas hasil export sesuai dataset dan format.
func (h *Handlers) ExchangeExport(w http.ResponseWriter, r *http.Request) {
	_, _, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	ds, ok := datasetInfo(r.URL.Query().Get("dataset"))
	if !ok {
		http.Error(w, "400 pilihan data tidak dikenal", http.StatusBadRequest)
		return
	}
	format, ok := exchange.FormatInfo(r.URL.Query().Get("format"))
	if !ok {
		http.Error(w, "400 format tidak dikenal", http.StatusBadRequest)
		return
	}

	from, to, err := exchangeRange(r)
	if err != nil {
		http.Error(w, "400 "+err.Error(), http.StatusBadRequest)
		return
	}

	table, err := h.buildTable(r, company, ds.Value, from, to)
	if err != nil {
		http.Error(w, "500 kesalahan server", http.StatusInternalServerError)
		return
	}

	filename := company.Slug + "-" + ds.Value + "-" + to.Format("20060102") + "." + format.Ext
	w.Header().Set("Content-Type", format.Mime)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("Cache-Control", "no-store")
	if err := exchange.Write(w, table, format.Value); err != nil {
		// Header sudah terkirim; catat saja kegagalan penulisan.
		return
	}
}

// ExchangeTemplate mengirim berkas contoh (header saja) untuk diisi pengguna.
func (h *Handlers) ExchangeTemplate(w http.ResponseWriter, r *http.Request) {
	// Perusahaan hanya dipakai untuk memastikan pengguna berhak membuka halaman.
	if _, _, _, ok := h.companyContext(w, r); !ok {
		return
	}
	ds, ok := datasetInfo(r.URL.Query().Get("dataset"))
	if !ok || !ds.Importable {
		http.Error(w, "400 pilihan data tidak bisa diimpor", http.StatusBadRequest)
		return
	}
	format, ok := exchange.FormatInfo(r.URL.Query().Get("format"))
	if !ok {
		http.Error(w, "400 format tidak dikenal", http.StatusBadRequest)
		return
	}
	table := emptyTemplate(ds.Value)
	filename := "contoh-" + ds.Value + "." + format.Ext
	w.Header().Set("Content-Type", format.Mime)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	if err := exchange.Write(w, table, format.Value); err != nil {
		return
	}
}

// ImportResult merangkum hasil pemeriksaan atau penerapan import.
type ImportResult struct {
	FileName     string
	Dataset      string
	DatasetLabel string
	Format       string
	TotalRows    int
	Groups       int
	Valid        int
	Errors       []string
	Warnings     []string
	Applied      bool
}

// ExchangeImportPost membaca berkas unggahan, memeriksa isinya, lalu
// menampilkan pratinjau sebelum diterapkan.
func (h *Handlers) ExchangeImportPost(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "400 berkas terlalu besar atau form tidak valid", http.StatusBadRequest)
		return
	}
	ds, dsOK := datasetInfo(r.FormValue("dataset"))
	if !dsOK || !ds.Importable {
		http.Error(w, "400 pilihan data tidak bisa diimpor", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("berkas")
	if err != nil {
		h.renderImportPreview(w, r, user, role, company, ImportResult{
			Dataset: ds.Value, DatasetLabel: ds.Label,
			Errors: []string{"Berkas belum dipilih."},
		}, "")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 8<<20))
	if err != nil {
		http.Error(w, "400 berkas tidak dapat dibaca", http.StatusBadRequest)
		return
	}
	table, err := exchange.Read(data, header.Filename)
	if err != nil {
		h.renderImportPreview(w, r, user, role, company, ImportResult{
			FileName: header.Filename, Dataset: ds.Value, DatasetLabel: ds.Label,
			Errors: []string{err.Error()},
		}, "")
		return
	}

	token, err := saveUpload(data, header.Filename)
	if err != nil {
		http.Error(w, "500 berkas unggahan tidak dapat disimpan", http.StatusInternalServerError)
		return
	}

	result := h.periksaImport(r, company, ds, table)
	result.FileName = header.Filename
	result.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(header.Filename)), ".")
	h.renderImportPreview(w, r, user, role, company, result, token)
}

// ExchangeImportConfirm menerapkan berkas yang sudah diperiksa.
func (h *Handlers) ExchangeImportConfirm(w http.ResponseWriter, r *http.Request) {
	user, role, company, ok := h.companyContext(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "400 form tidak valid", http.StatusBadRequest)
		return
	}
	ds, dsOK := datasetInfo(r.FormValue("dataset"))
	if !dsOK || !ds.Importable {
		http.Error(w, "400 pilihan data tidak bisa diimpor", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	data, name, err := loadUpload(token)
	if err != nil {
		http.Error(w, "410 berkas unggahan sudah tidak tersedia, ulangi unggahan", http.StatusGone)
		return
	}
	table, err := exchange.Read(data, name)
	if err != nil {
		http.Error(w, "400 "+err.Error(), http.StatusBadRequest)
		return
	}

	result := h.periksaImport(r, company, ds, table)
	result.FileName = name
	if len(result.Errors) == 0 && result.Valid > 0 {
		applied, errs := h.terapkanImport(r, company, ds, table, user.UserID)
		result.Applied = true
		result.Valid = applied
		result.Errors = append(result.Errors, errs...)
	}
	removeUpload(token)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.ExchangeResultPage(h.newShell(r, user, company, role, "exchange", "Hasil Import "+company.Name),
		company, templates.ExchangeResultView{
			FileName: result.FileName,
			Dataset:  result.DatasetLabel,
			Applied:  result.Applied,
			Count:    result.Valid,
			Errors:   result.Errors,
			Warnings: result.Warnings,
		}).Render(r.Context(), w)
}

func (h *Handlers) renderImportPreview(w http.ResponseWriter, r *http.Request, user appmw.SessionUser, role string,
	company db.Company, result ImportResult, token string) {
	semua, _ := exchangeDatasets()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(result.Errors) > 0 && !result.Applied && result.TotalRows == 0 {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = templates.ExchangePreviewPage(h.newShell(r, user, company, role, "exchange", "Periksa Import "+company.Name),
		company, templates.ExchangePreviewView{
			FileName:  result.FileName,
			Dataset:   result.Dataset,
			Label:     result.DatasetLabel,
			Format:    result.Format,
			TotalRows: result.TotalRows,
			Groups:    result.Groups,
			Valid:     result.Valid,
			Errors:    result.Errors,
			Warnings:  result.Warnings,
			Token:     token,
			Datasets:  semua,
			Formats:   exchangeFormatViews(),
		}).Render(r.Context(), w)
}

// ExchangeRangeFromQuery membaca rentang tanggal export. Rentang bawaan adalah
// seluruh catatan sampai hari ini.
func exchangeRange(r *http.Request) (time.Time, time.Time, error) {
	from := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Now()
	if v := strings.TrimSpace(r.URL.Query().Get("from")); v != "" {
		t, err := models.ParseDateID(v)
		if err != nil {
			return from, to, errors.New("tanggal awal harus berformat dd/mm/yyyy")
		}
		from = t
	}
	if v := strings.TrimSpace(r.URL.Query().Get("to")); v != "" {
		t, err := models.ParseDateID(v)
		if err != nil {
			return from, to, errors.New("tanggal akhir harus berformat dd/mm/yyyy")
		}
		to = t
	}
	if from.After(to) {
		return from, to, errors.New("tanggal awal melewati tanggal akhir")
	}
	return from, to, nil
}

// buildTable menyiapkan tabel data sesuai dataset dan rentang tanggal.
func (h *Handlers) buildTable(r *http.Request, company db.Company, name string, from, to time.Time) (*exchange.Table, error) {
	switch name {
	case "akun":
		rows, err := h.DB.ListAccountsAll(r.Context(), company.ID)
		if err != nil {
			return nil, err
		}
		table := exchange.NewTable("akun",
			[]string{"Kode", "Nama", "Jenis", "Sub Jenis", "Saldo Normal", "Pos PSAK 201", "Aktif"},
			[]bool{false, false, false, false, false, false, false})
		for _, a := range rows {
			aktif := "tidak"
			if a.IsActive {
				aktif = "ya"
			}
			table.AddRow(a.Code, a.Name, string(a.AccountType), a.SubType, string(a.NormalBalance),
				models.PsakLineLabel(a.PsakLine), aktif)
		}
		return table, nil

	case "jurnal":
		rows, err := h.exportLines(r, company.ID, from, to)
		if err != nil {
			return nil, err
		}
		table := exchange.NewTable("jurnal",
			[]string{"Tanggal", "Referensi", "Memo", "Jenis", "Penyesuaian", "Kode Akun", "Nama Akun", "Keterangan", "Debit", "Kredit"},
			[]bool{false, false, false, false, false, false, false, false, true, true})
		for _, e := range rows {
			adj := "tidak"
			if e.IsAdjusting {
				adj = "ya"
			}
			table.AddRow(exchange.FormatTanggal(e.JournalDate.Time), e.Reference, e.Memo,
				journalKindLabel(e.Kind), adj, e.Code, e.AccountName, e.Description,
				exchange.MoneyText(models.NumericToCents(e.Debit)), exchange.MoneyText(models.NumericToCents(e.Credit)))
		}
		return table, nil

	case "buku_besar":
		rows, err := h.exportLines(r, company.ID, from, to)
		if err != nil {
			return nil, err
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Code != rows[j].Code {
				return rows[i].Code < rows[j].Code
			}
			return rows[i].JournalDate.Time.Before(rows[j].JournalDate.Time)
		})
		table := exchange.NewTable("buku_besar",
			[]string{"Kode Akun", "Nama Akun", "Tanggal", "Referensi", "Keterangan", "Debit", "Kredit"},
			[]bool{false, false, false, false, false, true, true})
		for _, e := range rows {
			table.AddRow(e.Code, e.AccountName, exchange.FormatTanggal(e.JournalDate.Time), e.Reference,
				e.Description, exchange.MoneyText(models.NumericToCents(e.Debit)), exchange.MoneyText(models.NumericToCents(e.Credit)))
		}
		return table, nil

	default: // neraca_saldo
		inception := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
		rows, err := h.periodRows(r.Context(), company.ID, inception, to)
		if err != nil {
			return nil, err
		}
		table := exchange.NewTable("neraca_saldo",
			[]string{"Kode", "Nama", "Jenis", "Saldo Normal", "Debit", "Kredit", "Saldo"},
			[]bool{false, false, false, false, true, true, true})
		var totalDebit, totalCredit int64
		for _, row := range rows {
			debit := models.NumericToCents(row.TotalDebit)
			credit := models.NumericToCents(row.TotalCredit)
			if debit == 0 && credit == 0 {
				continue
			}
			saldo := debit - credit
			if row.NormalBalance == db.NormalBalanceCredit {
				saldo = credit - debit
			}
			totalDebit += debit
			totalCredit += credit
			table.AddRow(row.Code, row.Name, string(row.AccountType), string(row.NormalBalance),
				exchange.MoneyText(debit), exchange.MoneyText(credit), exchange.MoneyText(saldo))
		}
		table.AddRow("", "Jumlah", "", "", exchange.MoneyText(totalDebit), exchange.MoneyText(totalCredit), "")
		return table, nil
	}
}

func (h *Handlers) exportLines(r *http.Request, companyID int64, from, to time.Time) ([]db.ExportJournalLinesRow, error) {
	return h.DB.ExportJournalLines(r.Context(), db.ExportJournalLinesParams{
		CompanyID: companyID,
		DateFrom:  dateValue(from),
		DateTo:    dateValue(to),
	})
}

func journalKindLabel(kind string) string {
	switch kind {
	case "opening":
		return "Saldo awal"
	case "closing":
		return "Penutup"
	default:
		return "Umum"
	}
}

// emptyTemplate membuat berkas contoh berisi header dan satu baris petunjuk.
func emptyTemplate(name string) *exchange.Table {
	switch name {
	case "akun":
		t := exchange.NewTable("akun",
			[]string{"Kode", "Nama", "Jenis", "Sub Jenis", "Saldo Normal", "Pos PSAK 201", "Aktif"},
			[]bool{false, false, false, false, false, false, false})
		t.AddRow("1-1010", "Bank BCA", "aset", "aset_lancar", "debit", "Kas dan setara kas", "ya")
		return t
	default:
		t := exchange.NewTable("jurnal",
			[]string{"Tanggal", "Referensi", "Memo", "Kode Akun", "Keterangan", "Debit", "Kredit"},
			[]bool{false, false, false, false, false, true, true})
		t.AddRow("01/01/2026", "JV-0001", "Contoh jurnal", "1-1010", "Bank BCA", "100000.00", "0.00")
		t.AddRow("01/01/2026", "JV-0001", "Contoh jurnal", "4-4010", "Pendapatan", "0.00", "100000.00")
		return t
	}
}

// periksaImport memvalidasi isi berkas tanpa menyimpan apa pun.
func (h *Handlers) periksaImport(r *http.Request, company db.Company, ds dataset, table *exchange.Table) ImportResult {
	result := ImportResult{Dataset: ds.Value, DatasetLabel: ds.Label, TotalRows: len(table.Rows)}
	switch ds.Value {
	case "akun":
		kolom := []string{"Kode", "Nama", "Jenis", "Sub Jenis", "Saldo Normal"}
		if err := cekKolom(table, kolom); err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		akun, err := h.DB.ListAccountsAll(r.Context(), company.ID)
		if err != nil {
			result.Errors = append(result.Errors, "gagal membaca daftar akun")
			return result
		}
		ada := map[string]bool{}
		for _, a := range akun {
			ada[strings.ToUpper(strings.TrimSpace(a.Code))] = true
		}
		for i, row := range table.Rows {
			kode := strings.ToUpper(table.Nilai(row, "Kode"))
			nama := table.Nilai(row, "Nama")
			jenis := strings.ToLower(table.Nilai(row, "Jenis"))
			normal := strings.ToLower(table.Nilai(row, "Saldo Normal"))
			if kode == "" || nama == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("Baris %d: kode dan nama wajib diisi.", i+2))
				continue
			}
			if jenis != "aset" && jenis != "liability" && jenis != "equity" && jenis != "revenue" && jenis != "expense" {
				result.Errors = append(result.Errors, fmt.Sprintf("Baris %d: jenis akun %q tidak dikenal.", i+2, jenis))
				continue
			}
			if normal != "debit" && normal != "credit" {
				result.Errors = append(result.Errors, fmt.Sprintf("Baris %d: saldo normal harus debit atau credit.", i+2))
				continue
			}
			if ada[kode] {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Baris %d: akun %s sudah ada, dilewati.", i+2, kode))
				continue
			}
			ada[kode] = true
			result.Valid++
		}
		return result

	default: // jurnal
		if err := cekKolom(table, []string{"Tanggal", "Kode Akun", "Debit", "Kredit"}); err != nil {
			result.Errors = append(result.Errors, err.Error())
			return result
		}
		akun, err := h.DB.ListAccountsAll(r.Context(), company.ID)
		if err != nil {
			result.Errors = append(result.Errors, "gagal membaca daftar akun")
			return result
		}
		adaKode := map[string]bool{}
		for _, a := range akun {
			adaKode[strings.ToUpper(strings.TrimSpace(a.Code))] = true
		}

		// Referensi yang sudah ada dipakai untuk memperingatkan jurnal kembar.
		ada, err := h.DB.ListJournalKeys(r.Context(), company.ID)
		if err != nil {
			result.Errors = append(result.Errors, "gagal membaca daftar jurnal")
			return result
		}
		kunci := map[string]bool{}
		for _, k := range ada {
			kunci[k.JournalDate.Time.Format("2006-01-02")+"|"+strings.ToUpper(strings.TrimSpace(k.Reference))] = true
		}

		groups := groupJournalRows(table)
		result.Groups = len(groups)
		for _, g := range groups {
			if errs := periksaGrup(g, adaKode); len(errs) > 0 {
				result.Errors = append(result.Errors, errs...)
				continue
			}
			// Peringatan saja: import tetap diizinkan karena perusahaan bisa
			// saja memasukkan jurnal bernomor sama dari periode berbeda.
			if tgl, err := exchange.ParseTanggal(g.Date); err == nil {
				key := tgl.Format("2006-01-02") + "|" + strings.ToUpper(strings.TrimSpace(g.Reference))
				if kunci[key] {
					result.Warnings = append(result.Warnings, fmt.Sprintf(
						"Jurnal %s tanggal %s sudah ada di pembukuan ini; import akan menambah jurnal baru dengan nomor yang sama.",
						g.Reference, g.Date))
				}
			}
			result.Valid++
		}
		return result
	}
}

func cekKolom(table *exchange.Table, wajib []string) error {
	var hilang []string
	for _, k := range wajib {
		if table.Kolom(k) < 0 {
			hilang = append(hilang, k)
		}
	}
	if len(hilang) > 0 {
		return fmt.Errorf("kolom wajib tidak ditemukan: %s. Unduh contoh berkas pada halaman ini untuk melihat susunan kolom.",
			strings.Join(hilang, ", "))
	}
	return nil
}

// importLine adalah satu baris jurnal hasil pembacaan berkas import.
type importLine struct {
	Code        string
	Description string
	DebitCents  int64
	CreditCents int64
}

// journalGroup adalah satu jurnal hasil pengelompokan baris berkas import.
type journalGroup struct {
	Date       string
	Reference  string
	Memo       string
	Lines      []importLine
	RowNumbers []int
}

// groupJournalRows mengelompokkan baris menjadi jurnal berdasarkan kombinasi
// tanggal, referensi, dan memo yang berurutan.
func groupJournalRows(table *exchange.Table) []journalGroup {
	var groups []journalGroup
	idx := map[string]int{}
	for i, row := range table.Rows {
		tgl := table.Nilai(row, "Tanggal")
		ref := table.Nilai(row, "Referensi")
		memo := table.Nilai(row, "Memo")
		key := strings.ToUpper(tgl + "|" + ref + "|" + memo)
		pos, ok := idx[key]
		if !ok {
			groups = append(groups, journalGroup{Date: tgl, Reference: ref, Memo: memo})
			pos = len(groups) - 1
			idx[key] = pos
		}
		kode := table.Nilai(row, "Kode Akun")
		debit, _ := exchange.ParseMoneyColumn(table.Nilai(row, "Debit"))
		kredit, _ := exchange.ParseMoneyColumn(table.Nilai(row, "Kredit"))
		groups[pos].Lines = append(groups[pos].Lines, importLine{
			Code:        kode,
			Description: table.Nilai(row, "Keterangan"),
			DebitCents:  debit,
			CreditCents: kredit,
		})
		groups[pos].RowNumbers = append(groups[pos].RowNumbers, i+2)
	}
	return groups
}

// periksaGrup memvalidasi satu jurnal hasil grouping.
func periksaGrup(g journalGroup, adaKode map[string]bool) []string {
	label := g.Reference
	if label == "" {
		label = g.Date
	}
	baris := func() string {
		if len(g.RowNumbers) == 0 {
			return "-"
		}
		parts := make([]string, 0, len(g.RowNumbers))
		for _, n := range g.RowNumbers {
			parts = append(parts, strconv.Itoa(n))
		}
		return strings.Join(parts, ",")
	}
	var errs []string
	if _, err := exchange.ParseTanggal(g.Date); err != nil {
		errs = append(errs, fmt.Sprintf("Baris %s: tanggal %q harus berformat dd/mm/yyyy.", baris(), g.Date))
	}
	if strings.TrimSpace(g.Reference) == "" {
		errs = append(errs, fmt.Sprintf("Baris %s: referensi wajib diisi.", baris()))
	}
	if len(g.Lines) < 2 {
		errs = append(errs, fmt.Sprintf("Jurnal %s: minimal dua baris debit/kredit.", label))
	}
	var debit, kredit int64
	for _, l := range g.Lines {
		if !adaKode[strings.ToUpper(strings.TrimSpace(l.Code))] {
			errs = append(errs, fmt.Sprintf("Jurnal %s: kode akun %q tidak ada di daftar akun perusahaan ini.", label, l.Code))
		}
		debit += l.DebitCents
		kredit += l.CreditCents
	}
	if debit != kredit {
		errs = append(errs, fmt.Sprintf("Jurnal %s: total debit %s tidak sama dengan total kredit %s.",
			label, exchange.MoneyText(debit), exchange.MoneyText(kredit)))
	}
	if debit == 0 && kredit == 0 {
		errs = append(errs, fmt.Sprintf("Jurnal %s: tidak ada nilai debit atau kredit.", label))
	}
	return errs
}

// terapkanImport menyimpan hasil import ke database. Mengembalikan jumlah data
// yang tersimpan beserta pesan kesalahan yang muncul saat penyimpanan.
func (h *Handlers) terapkanImport(r *http.Request, company db.Company, ds dataset, table *exchange.Table, userID int64) (int, []string) {
	switch ds.Value {
	case "akun":
		akun, err := h.DB.ListAccountsAll(r.Context(), company.ID)
		if err != nil {
			return 0, []string{"gagal membaca daftar akun"}
		}
		ada := map[string]bool{}
		for _, a := range akun {
			ada[strings.ToUpper(strings.TrimSpace(a.Code))] = true
		}
		tersimpan := 0
		var errs []string
		for i, row := range table.Rows {
			kode := strings.TrimSpace(table.Nilai(row, "Kode"))
			key := strings.ToUpper(kode)
			if kode == "" || ada[key] {
				continue
			}
			nama := table.Nilai(row, "Nama")
			jenis := db.AccountType(strings.ToLower(table.Nilai(row, "Jenis")))
			sub := strings.TrimSpace(table.Nilai(row, "Sub Jenis"))
			normal := db.NormalBalance(strings.ToLower(table.Nilai(row, "Saldo Normal")))
			if sub == "" {
				sub = defaultSubType(jenis)
			}
			psak := strings.TrimSpace(table.Nilai(row, "Pos PSAK 201"))
			if psak == "" {
				psak = models.GuessPsakLine(nama, string(jenis), sub, "")
			}
			if _, err := h.DB.CreateAccount(r.Context(), db.CreateAccountParams{
				CompanyID:     company.ID,
				Code:          kode,
				Name:          nama,
				AccountType:   jenis,
				SubType:       sub,
				NormalBalance: normal,
				PsakLine:      psak,
			}); err != nil {
				errs = append(errs, fmt.Sprintf("Baris %d: gagal menyimpan akun %s (%v).", i+2, kode, err))
				continue
			}
			ada[key] = true
			tersimpan++
		}
		return tersimpan, errs

	default:
		akun, err := h.DB.ListAccountsAll(r.Context(), company.ID)
		if err != nil {
			return 0, []string{"gagal membaca daftar akun"}
		}
		ids := map[string]int64{}
		for _, a := range akun {
			ids[strings.ToUpper(strings.TrimSpace(a.Code))] = a.ID
		}
		tersimpan := 0
		var errs []string
		for _, g := range groupJournalRows(table) {
			if errsGrup := periksaGrup(g, mapKeySet(ids)); len(errsGrup) > 0 {
				errs = append(errs, errsGrup...)
				continue
			}
			tgl, _ := exchange.ParseTanggal(g.Date)
			params := db.CreateJournalWithEntriesParams{
				CompanyID:    company.ID,
				JournalDate:  pgtype.Date{Time: tgl, Valid: true, InfinityModifier: pgtype.Finite},
				Reference:    strings.TrimSpace(g.Reference),
				Memo:         strings.TrimSpace(g.Memo),
				CreatedBy:    userID,
				AccountIds:   make([]int64, len(g.Lines)),
				Descriptions: make([]string, len(g.Lines)),
				Debits:       make([]pgtype.Numeric, len(g.Lines)),
				Credits:      make([]pgtype.Numeric, len(g.Lines)),
			}
			for i, l := range g.Lines {
				params.AccountIds[i] = ids[strings.ToUpper(strings.TrimSpace(l.Code))]
				params.Descriptions[i] = l.Description
				params.Debits[i], _ = models.CentsToNumeric(l.DebitCents)
				params.Credits[i], _ = models.CentsToNumeric(l.CreditCents)
			}
			if _, err := h.DB.CreateJournalWithEntries(r.Context(), params); err != nil {
				errs = append(errs, fmt.Sprintf("Jurnal %s gagal disimpan: %v", g.Reference, err))
				continue
			}
			tersimpan++
		}
		return tersimpan, errs
	}
}

func mapKeySet(m map[string]int64) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func defaultSubType(jenis db.AccountType) string {
	switch jenis {
	case db.AccountTypeAsset:
		return models.SubCurrentAsset
	case db.AccountTypeLiability:
		return models.SubCurrentLiability
	case db.AccountTypeEquity:
		return models.SubEquity
	case db.AccountTypeRevenue:
		return models.SubRevenue
	default:
		return models.SubExpense
	}
}

// saveUpload menyimpan berkas unggahan sementara dan mengembalikan tokennya.
func saveUpload(data []byte, name string) (string, error) {
	dir := exchangeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	target := filepath.Join(dir, token+".upload")
	meta := filepath.Join(dir, token+".name")
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(meta, []byte(filepath.Base(name)), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

// loadUpload membaca kembali berkas unggahan berdasarkan token.
func loadUpload(token string) ([]byte, string, error) {
	if len(token) != 32 {
		return nil, "", errors.New("token tidak valid")
	}
	if _, err := hex.DecodeString(token); err != nil {
		return nil, "", errors.New("token tidak valid")
	}
	dir := exchangeDir()
	data, err := os.ReadFile(filepath.Join(dir, token+".upload"))
	if err != nil {
		return nil, "", err
	}
	name := "unggahan"
	if raw, err := os.ReadFile(filepath.Join(dir, token+".name")); err == nil {
		name = strings.TrimSpace(string(raw))
	}
	return data, name, nil
}

func removeUpload(token string) {
	if len(token) != 32 {
		return
	}
	dir := exchangeDir()
	_ = os.Remove(filepath.Join(dir, token+".upload"))
	_ = os.Remove(filepath.Join(dir, token+".name"))
}

// pruneUploads menghapus berkas unggahan yang lebih tua dari sehari.
func PruneUploads() {
	dir := exchangeDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	batas := time.Now().Add(-24 * time.Hour)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.ModTime().After(batas) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
