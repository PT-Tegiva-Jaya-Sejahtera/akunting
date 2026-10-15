package exchange

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// Read membaca tabel dari data mentah. Format boleh ditentukan lewat nama
// berkas, dan bila kosong akan dideteksi otomatis dari isi.
func Read(data []byte, filename string) (*Table, error) {
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	switch format {
	case FormatCSV, FormatTXT:
		return readDelimited(data, format)
	case FormatXML:
		return readXML(data)
	case FormatXLS:
		return readSpreadsheetML(data)
	case FormatXLSX:
		return readXLSX(data)
	}
	// Deteksi isi.
	trim := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	switch {
	case len(trim) > 1 && trim[0] == 'P' && trim[1] == 'K':
		return readXLSX(data)
	case bytes.HasPrefix(trim, []byte("<?xml")), bytes.HasPrefix(trim, []byte("<")):
		if bytes.Contains(trim[:min(len(trim), 400)], []byte("<Workbook")) {
			return readSpreadsheetML(data)
		}
		return readXML(data)
	default:
		return readDelimited(data, FormatCSV)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// readDelimited membaca CSV/TXT sambil menebak pemisah kolom.
func readDelimited(data []byte, format string) (*Table, error) {
	text := strings.TrimPrefix(string(data), "\uFEFF")
	delim := ','
	if format == FormatTXT {
		delim = '\t'
	} else {
		first := text
		if i := strings.IndexAny(text, "\r\n"); i >= 0 {
			first = text[:i]
		}
		semi := strings.Count(first, ";")
		tab := strings.Count(first, "\t")
		kom := strings.Count(first, ",")
		switch {
		case tab > kom && tab > semi:
			delim = '\t'
		case semi > kom:
			delim = ';'
		}
	}
	cr := csv.NewReader(strings.NewReader(text))
	cr.Comma = delim
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("gagal membaca berkas terpisah: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("berkas kosong")
	}
	t := &Table{Name: "import"}
	t.Header = records[0]
	for _, rec := range records[1:] {
		if isBlankRow(rec) {
			continue
		}
		row := make([]string, len(t.Header))
		copy(row, rec)
		t.Rows = append(t.Rows, row)
	}
	return t, nil
}

func isBlankRow(rec []string) bool {
	for _, v := range rec {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// readXML membaca skema pertukaran <akunting><table>.
func readXML(data []byte) (*Table, error) {
	var doc xmlDoc
	if err := xml.Unmarshal(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}), &doc); err != nil {
		return nil, fmt.Errorf("XML tidak dikenali: %w", err)
	}
	if len(doc.Tables) == 0 {
		return nil, fmt.Errorf("XML tidak memuat tabel data")
	}
	xt := doc.Tables[0]
	t := &Table{Name: xt.Name}
	t.Header = make([]string, 0, len(xt.Columns))
	idx := map[int]int{}
	for pos, c := range xt.Columns {
		t.Header = append(t.Header, c.Title)
		idx[c.Index] = pos
	}
	if len(t.Header) == 0 {
		return nil, fmt.Errorf("XML tidak memuat daftar kolom")
	}
	for _, xr := range xt.Rows {
		row := make([]string, len(t.Header))
		for _, f := range xr.Fields {
			if pos, ok := idx[f.Index]; ok {
				row[pos] = f.Value
			}
		}
		if isBlankRow(row) {
			continue
		}
		t.Rows = append(t.Rows, row)
	}
	return t, nil
}

// readSpreadsheetML membaca berkas .xls gaya SpreadsheetML 2003.
func readSpreadsheetML(data []byte) (*Table, error) {
	type ssCellIn struct {
		Data struct {
			Type  string `xml:"Type,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
		Index int `xml:"Index,attr"`
	}
	var wb struct {
		Worksheets []struct {
			Name  string `xml:"Name,attr"`
			Table struct {
				Rows []struct {
					Cells []ssCellIn `xml:"Cell"`
				} `xml:"Row"`
			} `xml:"Table"`
		} `xml:"Worksheet"`
	}
	dec := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})))
	if err := dec.Decode(&wb); err != nil {
		return nil, fmt.Errorf("berkas .xls tidak dikenali: %w", err)
	}
	if len(wb.Worksheets) == 0 {
		return nil, fmt.Errorf("berkas .xls tidak memuat lembar kerja")
	}
	t := &Table{Name: wb.Worksheets[0].Name}
	rows := wb.Worksheets[0].Table.Rows
	if len(rows) == 0 {
		return nil, fmt.Errorf("berkas .xls kosong")
	}
	for _, c := range rows[0].Cells {
		t.Header = append(t.Header, strings.TrimSpace(c.Data.Value))
	}
	for _, r := range rows[1:] {
		row := make([]string, 0, len(r.Cells))
		for _, c := range r.Cells {
			row = append(row, strings.TrimSpace(c.Data.Value))
		}
		if isBlankRow(row) {
			continue
		}
		padded := make([]string, len(t.Header))
		copy(padded, row)
		t.Rows = append(t.Rows, padded)
	}
	return t, nil
}

// readXLSX membaca lembar pertama berkas OOXML (.xlsx).
func readXLSX(data []byte) (*Table, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("berkas .xlsx tidak dapat dibuka: %w", err)
	}
	var shared []string
	var sheetXML []byte
	for _, f := range zr.File {
		switch f.Name {
		case "xl/sharedStrings.xml":
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			raw, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			shared = parseSharedStrings(raw)
		case "xl/worksheets/sheet1.xml":
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			sheetXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	if sheetXML == nil {
		return nil, fmt.Errorf("berkas .xlsx tidak memuat lembar kerja pertama")
	}
	return parseXLSXSheet(sheetXML, shared)
}

func parseSharedStrings(raw []byte) []string {
	var sst struct {
		Items []struct {
			Text string `xml:",chardata"`
			Runs []struct {
				Text string `xml:",chardata"`
			} `xml:"r>t"`
		} `xml:"si"`
	}
	if err := xml.Unmarshal(raw, &sst); err != nil {
		return nil
	}
	out := make([]string, 0, len(sst.Items))
	for _, it := range sst.Items {
		if len(it.Runs) > 0 {
			var b strings.Builder
			for _, r := range it.Runs {
				b.WriteString(r.Text)
			}
			out = append(out, b.String())
			continue
		}
		out = append(out, it.Text)
	}
	return out
}

func parseXLSXSheet(raw []byte, shared []string) (*Table, error) {
	type cellIn struct {
		Ref  string `xml:"r,attr"`
		Type string `xml:"t,attr"`
		V    string `xml:"v"`
		IS   struct {
			T string `xml:"t"`
		} `xml:"is"`
	}
	var sheet struct {
		Rows []struct {
			Cells []cellIn `xml:"c"`
		} `xml:"sheetData>row"`
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&sheet); err != nil {
		return nil, fmt.Errorf("lembar kerja .xlsx tidak terbaca: %w", err)
	}
	t := &Table{Name: "import"}
	grid := make([][]string, 0, len(sheet.Rows))
	width := 0
	for _, r := range sheet.Rows {
		row := []string{}
		for _, c := range r.Cells {
			letters := strings.TrimRight(c.Ref, "0123456789")
			col := columnIndex(letters)
			for len(row) <= col {
				row = append(row, "")
			}
			val := strings.TrimSpace(c.V)
			switch c.Type {
			case "s":
				if n, err := strconv.Atoi(val); err == nil && n >= 0 && n < len(shared) {
					val = shared[n]
				}
			case "inlineStr":
				val = strings.TrimSpace(c.IS.T)
			}
			row[col] = val
		}
		if isBlankRow(row) {
			continue
		}
		if len(row) > width {
			width = len(row)
		}
		grid = append(grid, row)
	}
	if len(grid) == 0 {
		return nil, fmt.Errorf("lembar kerja kosong")
	}
	t.Header = make([]string, width)
	copy(t.Header, grid[0])
	for _, r := range grid[1:] {
		padded := make([]string, width)
		copy(padded, r)
		t.Rows = append(t.Rows, padded)
	}
	return t, nil
}

// Kolom mengembalikan posisi kolom berdasarkan judul (tidak peka huruf besar
// kecil dan spasi ganda). -1 bila tidak ditemukan.
func (t *Table) Kolom(judul string) int {
	want := normalisasi(judul)
	for i, h := range t.Header {
		if normalisasi(h) == want {
			return i
		}
	}
	return -1
}

// Nilai mengambil isi kolom pada satu baris; "" bila kolom tidak ada.
func (t *Table) Nilai(row []string, judul string) string {
	i := t.Kolom(judul)
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func normalisasi(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", " ")
	return strings.Join(strings.Fields(s), " ")
}
